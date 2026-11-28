"""The REST bridge over a legacy SOAP claims system that soap-claims-hub tests.

Clients speak JSON; the claims system (a mock per claim type, by its name) speaks
SOAP. The bridge builds the envelope, calls the claims system, and turns what
comes back into JSON.

  POST /claims                 {claimType, policyNumber, lossType, amount}
                               header X-Soap-Version: 1.1 (default) | 1.2
                               201 {claimId, decision, ...} | 400 | 502 {fault}
  GET  /claims/{id}?claimType= claim status
  POST /soap/{claimType}       a raw envelope, forwarded with the caller's headers
  GET  /health

The rule the suite exists to protect: a policy number must carry its claim type's
code (MED-1024 for a medical claim), checked in validate() before any envelope is
built. Take that out and claims-smoke, which submits a dental policy as a medical
claim, fails. A request the claims system refuses, or answers with a SOAP Fault, is
never reported as a success (from_soap()).

The claims system answers in SOAP 1.1 whatever version the request used.
"""
import json
import os
import re
import threading
import urllib.error
import urllib.request
import xml.etree.ElementTree as ET
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

CLAIM_TYPES = [t for t in os.environ.get("CLAIM_TYPES", "medical,dental,vision,pharmacy").split(",") if t]
CODES = {"medical": "MED", "dental": "DEN", "vision": "VIS", "pharmacy": "PHM"}
VERSIONS = [v for v in os.environ.get("SOAP_VERSIONS", "1.1,1.2").split(",") if v]
VALIDATION = os.environ.get("VALIDATION_LEVEL", "strict")  # strict | lenient | audit
NS11, NS12 = "http://schemas.xmlsoap.org/soap/envelope/", "http://www.w3.org/2003/05/soap-envelope"

lock = threading.Lock()
claims = {}


def envelope(version, inner):
    ns = NS12 if version == "1.2" else NS11
    return ('<?xml version="1.0" encoding="UTF-8"?>\n<soapenv:Envelope xmlns:soapenv="%s" xmlns:clm="urn:claims:v1">'
            '<soapenv:Header/><soapenv:Body>%s</soapenv:Body></soapenv:Envelope>' % (ns, inner))


def escape(text):
    return str(text).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def call_soap(claim_type, version, action, xml, content_type=None):
    """Posts an envelope to the claims system. Returns (status, body text)."""
    if content_type is None:
        content_type = ('application/soap+xml; charset=utf-8; action="%s"' % action) if version == "1.2" else "text/xml; charset=utf-8"
    headers = {"Content-Type": content_type}
    if action:
        headers["SOAPAction"] = action
    request = urllib.request.Request("http://%s-claims-mock:9080/ClaimService" % claim_type, data=xml.encode(), headers=headers, method="POST")
    try:
        with urllib.request.urlopen(request, timeout=10) as reply:
            return reply.status, reply.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as err:
        return err.code, err.read().decode("utf-8", "replace")
    except (urllib.error.URLError, OSError) as err:
        return 0, str(err)


def fields(xml):
    """Local-name -> text of every element of an envelope."""
    out = {}
    for element in ET.fromstring(xml).iter():
        out[element.tag.rsplit("}", 1)[-1]] = (element.text or "").strip()
    return out


def from_soap(status, body):
    """The JSON answer for what the claims system returned."""
    if status == 0:
        return 502, {"error": "the claims system did not answer"}
    try:
        parsed = fields(body)
    except ET.ParseError:
        return 502, {"error": "the claims system answered something that is not SOAP", "status": status}
    if "Fault" in parsed:  # the rule: a Fault is never a success
        return 502, {"fault": parsed.get("faultstring") or parsed.get("Text") or "SOAP fault", "faultcode": parsed.get("faultcode", "")}
    if status >= 400:
        return 400, {"error": "the claims system refused the request", "status": status}
    return 200, parsed


def validate(body):
    """A list of what is wrong with a claim, empty when it is fine."""
    problems = []
    if not isinstance(body, dict):
        return ["the body must be a JSON object"]
    claim_type = body.get("claimType")
    if claim_type not in CLAIM_TYPES:
        problems.append("claimType must be one of %s" % ",".join(CLAIM_TYPES))
    policy = str(body.get("policyNumber", ""))
    code = CODES.get(claim_type, "UNK")
    if VALIDATION == "strict" and not re.fullmatch(r"%s-\d+" % code, policy):
        problems.append("policyNumber must look like %s-<digits>" % code)
    if not body.get("lossType"):
        problems.append("lossType is required")
    if not isinstance(body.get("amount"), int) or isinstance(body.get("amount"), bool) or body["amount"] <= 0:
        problems.append("amount must be a positive integer")
    return problems


def submit(body, version):
    problems = validate(body)
    if problems:
        return 400, {"errors": problems}
    inner = ("<clm:SubmitClaimRequest><clm:PolicyNumber>%s</clm:PolicyNumber><clm:LossType>%s</clm:LossType><clm:Amount>%d</clm:Amount></clm:SubmitClaimRequest>"
             % (escape(body["policyNumber"]), escape(body["lossType"]), body["amount"]))
    status, answer = from_soap(*call_soap(body["claimType"], version, "urn:SubmitClaim", envelope(version, inner)))
    if status != 200:
        return status, answer
    record = {"claimId": answer.get("ClaimId", ""), "decision": answer.get("Decision", ""), "claimType": body["claimType"],
              "soapVersion": version, "traceId": answer.get("TraceId", "")}
    with lock:
        claims[record["claimId"]] = record
    return 201, record


def status_of(claim_id, claim_type):
    if claim_type not in CLAIM_TYPES:
        return 400, {"errors": ["claimType must be one of %s" % ",".join(CLAIM_TYPES)]}
    inner = "<clm:GetClaimStatusRequest><clm:ClaimId>%s</clm:ClaimId></clm:GetClaimStatusRequest>" % escape(claim_id)
    status, answer = from_soap(*call_soap(claim_type, "1.1", "urn:GetClaimStatus", envelope("1.1", inner)))
    if status != 200:
        return status, answer
    return 200, {"claimId": answer.get("ClaimId", claim_id), "status": answer.get("Status", ""), "owner": answer.get("Owner", "")}


class Server(ThreadingHTTPServer):
    request_queue_size = 256
    daemon_threads = True


class Handler(BaseHTTPRequestHandler):
    server_version = "claims-bridge"

    def reply(self, status, body):
        payload = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def read_body(self):
        return self.rfile.read(int(self.headers.get("Content-Length") or 0))

    def do_GET(self):
        url = urlparse(self.path)
        if url.path == "/health":
            return self.reply(200, {"status": "ok"})
        if url.path.startswith("/claims/"):
            query = parse_qs(url.query)
            return self.reply(*status_of(url.path.rsplit("/", 1)[1], (query.get("claimType") or [""])[0]))
        self.reply(404, {"error": "not found"})

    def do_POST(self):
        url = urlparse(self.path)
        if url.path == "/claims":
            version = self.headers.get("X-Soap-Version", "1.1")
            if version not in VERSIONS:
                return self.reply(400, {"errors": ["unsupported SOAP version %s (supported: %s)" % (version, ",".join(VERSIONS))]})
            try:
                body = json.loads(self.read_body() or b"null")
            except ValueError:
                body = None
            return self.reply(*submit(body, version))
        if url.path.startswith("/soap/"):
            claim_type = url.path.rsplit("/", 1)[1]
            if claim_type not in CLAIM_TYPES:
                return self.reply(400, {"errors": ["unknown claim type"]})
            action = self.headers.get("SOAPAction", "")
            if not action:
                return self.reply(400, {"errors": ["a SOAPAction header is required"]})
            status, body = call_soap(claim_type, "1.1", action, self.read_body().decode("utf-8", "replace"),
                                     content_type=self.headers.get("Content-Type", "text/xml"))
            return self.reply(*from_soap(status, body))
        self.reply(404, {"error": "not found"})

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    print("claims-bridge on %d: claim types %s, SOAP %s, validation %s" % (port, CLAIM_TYPES, VERSIONS, VALIDATION), flush=True)
    Server(("0.0.0.0", port), Handler).serve_forever()

import json
import os
import sys
import urllib.error
import urllib.request
from xml.sax.saxutils import escape

API = os.environ.get("API_URL", "http://claims-bridge:8080")
cases = []


def call(method, path, body=None, headers=None, base=None, raw=False):
    data = (body.encode() if isinstance(body, str) else body) if raw else (None if body is None else json.dumps(body).encode())
    sent = {"Content-Type": "application/json"}
    sent.update(headers or {})
    request = urllib.request.Request((base or API) + path, data=data, method=method, headers=sent)
    try:
        with urllib.request.urlopen(request, timeout=15) as reply:
            text = reply.read()
            return reply.status, (text.decode("utf-8", "replace") if raw else json.loads(text or b"{}"))
    except urllib.error.HTTPError as err:
        text = err.read()
        try:
            return err.code, (text.decode("utf-8", "replace") if raw else json.loads(text or b"{}"))
        except ValueError:
            return err.code, text.decode("utf-8", "replace")


def check(name, fn):
    """Runs one check. A failed assertion is a failed test case, never a skipped one."""
    try:
        fn()
        cases.append((name, None))
    except AssertionError as err:
        cases.append((name, str(err) or "assertion failed"))
    except Exception as err:  # the service not answering is a failure too
        cases.append((name, "%s: %s" % (type(err).__name__, err)))
    print(("ok   " if cases[-1][1] is None else "FAIL ") + name + ("" if cases[-1][1] is None else " - " + cases[-1][1]), flush=True)


def finish(path, suite):
    """Writes the JUnit report for the checks that ran, and exits non-zero if one failed."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    failed = [case for case in cases if case[1]]
    lines = ['<?xml version="1.0" encoding="UTF-8"?>',
             '<testsuite name="%s" tests="%d" failures="%d" errors="0" skipped="0">' % (escape(suite), len(cases), len(failed))]
    for name, error in cases:
        if error:
            lines.append('<testcase classname="%s" name="%s"><failure message="%s"/></testcase>' % (escape(suite), escape(name), escape(error, {'"': "&quot;"})))
        else:
            lines.append('<testcase classname="%s" name="%s"/>' % (escape(suite), escape(name)))
    lines.append("</testsuite>")
    with open(path, "w", encoding="utf-8") as handle:
        handle.write("\n".join(lines))
    sys.exit(1 if failed else 0)


import re

CODES = {"medical": "MED", "dental": "DEN", "vision": "VIS", "pharmacy": "PHM"}
NS11 = "http://schemas.xmlsoap.org/soap/envelope/"


def claim(claim_type, amount=4200, policy=None, loss="water"):
    return {"claimType": claim_type, "policyNumber": policy or "%s-1024" % CODES[claim_type], "lossType": loss, "amount": amount}


def envelope(inner):
    return ('<?xml version="1.0" encoding="UTF-8"?><soapenv:Envelope xmlns:soapenv="%s" xmlns:clm="urn:claims:v1">'
            "<soapenv:Header/><soapenv:Body>%s</soapenv:Body></soapenv:Envelope>" % (NS11, inner))


SUBMIT = "<clm:SubmitClaimRequest><clm:PolicyNumber>MED-1024</clm:PolicyNumber><clm:LossType>water</clm:LossType><clm:Amount>4200</clm:Amount></clm:SubmitClaimRequest>"
STATUS = "<clm:GetClaimStatusRequest><clm:ClaimId>clm_2048</clm:ClaimId></clm:GetClaimStatusRequest>"

# The operations of api/wsdl/claims.wsdl, with what a caller must get back.
def submit_claim():
    status, record = call("POST", "/claims", claim("medical"))
    assert status == 201, "SubmitClaim gave %s %s" % (status, record)
    for field in ("claimId", "decision", "traceId"):
        assert record.get(field), "SubmitClaim's answer lacks %s: %s" % (field, record)


def get_claim_status():
    status, body = call("GET", "/claims/clm_2048?claimType=medical")
    assert status == 200, "GetClaimStatus gave %s %s" % (status, body)
    assert body["claimId"] == "clm_2048" and body["status"] == "IN_REVIEW" and body["owner"] == "manual_queue", "GetClaimStatus does not carry the mock example's data: %s" % body


def only_the_wsdl_operations_exist():
    status, body = call("POST", "/soap/medical", envelope(SUBMIT), headers={"SOAPAction": "urn:NotInTheWSDL", "Content-Type": "text/xml"}, raw=True)
    assert status == 502 and "fault" in body, "an operation that is not in the WSDL gave %s %s, want a SOAP Fault as a 502" % (status, body)


check("SubmitClaim answers with a claim id, a decision and a trace id", submit_claim)
check("GetClaimStatus answers with the claim's status and owner", get_claim_status)
check("an operation that is not in the WSDL is not served", only_the_wsdl_operations_exist)
finish("reports/adapter-contract.xml", "adapter-contract")

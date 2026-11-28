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

CLAIM_TYPE = os.environ.get("CLAIM_TYPE", "medical")
CLAIM_CODE = os.environ.get("CLAIM_CODE", CODES.get(CLAIM_TYPE, "UNK"))
state = {}


def a_claim_is_submitted_through_the_claims_system():
    status, record = call("POST", "/claims", claim(CLAIM_TYPE, policy=CLAIM_CODE + "-1024"))
    assert status == 201, "a claim gave %s %s" % (status, record)
    assert re.fullmatch(r"clm_\d+", record["claimId"]) and record["decision"] == "APPROVED", "the answer is not the claims system's: %s" % record
    assert record["traceId"], "no trace id came back from the claims system"
    state["id"] = record["claimId"]


def its_status_can_be_read():
    status, body = call("GET", "/claims/%s?claimType=%s" % (state["id"], CLAIM_TYPE))
    assert status == 200 and body["status"] and body["owner"], "the status gave %s %s" % (status, body)


def a_policy_of_another_claim_type_is_refused():
    other = next(code for kind, code in CODES.items() if kind != CLAIM_TYPE)
    status, body = call("POST", "/claims", claim(CLAIM_TYPE, policy=other + "-1024"))
    if status == 201:
        print("INVALID_CLAIM_CODE a %s policy was accepted for a %s claim" % (other, CLAIM_TYPE))
    assert status == 400, "a %s policy for a %s claim gave %s %s, want 400" % (other, CLAIM_TYPE, status, body)


def a_malformed_claim_is_refused():
    for bad in (dict(claim(CLAIM_TYPE), amount=-5), dict(claim(CLAIM_TYPE), amount="lots"), dict(claim(CLAIM_TYPE), lossType="")):
        status, body = call("POST", "/claims", bad)
        if status == 201:
            print("MISSING_REQUIRED_FIELD a malformed claim was accepted: %s" % bad)
        assert status == 400, "%s gave %s %s, want 400" % (bad, status, body)


check(CLAIM_TYPE + ": a claim is submitted through the claims system", a_claim_is_submitted_through_the_claims_system)
check(CLAIM_TYPE + ": its status can be read back", its_status_can_be_read)
check(CLAIM_TYPE + ": another claim type's policy is refused", a_policy_of_another_claim_type_is_refused)
check(CLAIM_TYPE + ": a malformed claim is refused", a_malformed_claim_is_refused)
finish("reports/%s-junit.xml" % CLAIM_TYPE, "claims-smoke-" + CLAIM_TYPE)

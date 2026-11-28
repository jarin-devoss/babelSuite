import json
import os
import sys
import urllib.error
import urllib.request
from xml.sax.saxutils import escape

API = os.environ.get("API_URL", "http://refund-worker:8080")
cases = []


def call(method, path, body=None, headers=None, base=None, raw=False):
    data = body if raw else (None if body is None else json.dumps(body).encode())
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


LIMIT = int(os.environ.get("REFUND_LIMIT_CENTS", "10000"))


def new_return(order, amount, reason):
    return call("POST", "/returns", {"orderId": order, "amountCents": amount, "currency": "USD", "returnReason": reason})

def a_reviewer_can_approve_a_held_return():
    status, record = new_return("ord_5000", 15900, "suspicious_activity")
    assert status == 202, "setup gave %s %s" % (status, record)
    status, approved = call("POST", "/returns/%s/approve" % record["returnId"])
    assert status == 200 and approved["status"] == "approved" and approved["refundCents"] == 15900, "approval gave %s %s" % (status, approved)
    status, again = call("POST", "/returns/%s/approve" % record["returnId"])
    assert status == 409, "approving twice gave %s %s, want 409" % (status, again)


def an_instant_refund_cannot_be_approved_again():
    status, record = new_return("ord_5001", min(4200, LIMIT), "damaged")
    assert status == 201, "setup gave %s %s" % (status, record)
    status, body = call("POST", "/returns/%s/approve" % record["returnId"])
    assert status == 409, "approving a return that was never held gave %s %s, want 409" % (status, body)


def an_unknown_return_is_a_404():
    status, _ = call("POST", "/returns/ret_missing/approve")
    assert status == 404, "approving an unknown return gave %s, want 404" % status
    status, _ = call("GET", "/returns/ret_missing")
    assert status == 404, "reading an unknown return gave %s, want 404" % status


check("a reviewer can approve a held return, once", a_reviewer_can_approve_a_held_return)
check("a return that was refunded at once cannot be approved", an_instant_refund_cannot_be_approved_again)
check("an unknown return is a 404", an_unknown_return_is_a_404)
finish("reports/junit.xml", "refund-smoke")

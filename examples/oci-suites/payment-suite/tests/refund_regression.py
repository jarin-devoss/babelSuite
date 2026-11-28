import json
import os
import sys
import urllib.error
import urllib.request
from xml.sax.saxutils import escape

API = os.environ.get("API_URL", "http://payment-api:8080")
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


def card(amount, currency="USD", token="tok_visa", merchant="m-117"):
    return {"amount": amount, "currency": currency, "cardToken": token, "merchantId": merchant}


def balance(currency):
    return call("GET", "/ledger?currency=" + currency)[1]["balance"]

def refund_more_than_authorized():
    status, payment = call("POST", "/payments", card(5000, currency="EUR"))
    assert status == 201, "setup payment gave %s" % status
    before = balance("EUR")
    status, body = call("POST", "/refunds", {"paymentId": payment["paymentId"], "amount": 5001})
    assert status == 422, "refunding more than was paid gave %s %s, want 422" % (status, body)
    assert balance("EUR") == before, "a refused refund changed the ledger"


def refund_twice():
    status, payment = call("POST", "/payments", card(3000, currency="EUR"))
    assert status == 201, "setup payment gave %s" % status
    status, _ = call("POST", "/refunds", {"paymentId": payment["paymentId"], "amount": 3000})
    assert status == 201, "refunding the whole payment gave %s" % status
    before = balance("EUR")
    status, body = call("POST", "/refunds", {"paymentId": payment["paymentId"], "amount": 1})
    assert status == 422, "a second refund of a refunded payment gave %s %s, want 422" % (status, body)
    assert balance("EUR") == before, "a refused refund changed the ledger"


def refund_unknown_payment():
    status, body = call("POST", "/refunds", {"paymentId": "pay_missing", "amount": 100})
    assert status == 404, "refunding an unknown payment gave %s %s, want 404" % (status, body)


def refund_a_parked_payment():
    status, payment = call("POST", "/payments", card(8000, currency="EUR", token="tok_risky", merchant="m-441"))
    if status != 202:
        return  # a strict strategy declines it, so there is nothing to refund
    status, body = call("POST", "/refunds", {"paymentId": payment["paymentId"], "amount": 100})
    assert status == 422, "refunding a payment that was never authorized gave %s %s, want 422" % (status, body)


check("refunding more than was authorized is refused and leaves the ledger alone", refund_more_than_authorized)
check("a refunded payment cannot be refunded again", refund_twice)
check("refunding an unknown payment is a 404", refund_unknown_payment)
check("a payment parked for review cannot be refunded", refund_a_parked_payment)
finish("reports/refund-regression.xml", "refund-regression")

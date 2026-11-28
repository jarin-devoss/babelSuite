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

STRATEGY = os.environ.get("FRAUD_STRATEGY", "standard")


def health():
    status, body = call("GET", "/health")
    assert status == 200 and body.get("status") == "ok", "health answered %s %s" % (status, body)


def authorized_payment_is_posted():
    before = balance("USD")
    status, payment = call("POST", "/payments", card(1299))
    assert status == 201 and payment.get("status") == "authorized", "an approved card gave %s %s" % (status, payment)
    status, again = call("GET", "/payments/" + payment["paymentId"])
    assert status == 200 and again["amount"] == 1299, "the payment cannot be read back: %s %s" % (status, again)
    assert balance("USD") == before + 1299, "the ledger did not take the authorized payment"


def risky_card_does_not_reach_the_ledger():
    before = balance("USD")
    status, body = call("POST", "/payments", card(9900, token="tok_risky", merchant="m-441"))
    if STRATEGY == "strict":
        assert status == 402, "strict fraud strategy must decline a payment parked for review, got %s %s" % (status, body)
    else:
        assert status == 202 and body.get("status") == "pending_review", "a risky card gave %s %s" % (status, body)
    assert balance("USD") == before, "a payment that was not authorized reached the ledger"


def invalid_payments_are_refused():
    for body in (card(1299, currency="US"), card(-5), card(0), {"amount": 100}):
        status, answer = call("POST", "/payments", body)
        assert status == 400, "%s gave %s %s, want 400" % (body, status, answer)


check("service answers /health", health)
check("an approved card is authorized, readable and in the ledger", authorized_payment_is_posted)
check("a risky card is parked (or declined under a strict strategy) and not in the ledger", risky_card_does_not_reach_the_ledger)
check("an invalid payment is refused", invalid_payments_are_refused)
finish("reports/junit.xml", "checkout-smoke")

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

CURRENCY = os.environ.get("TARGET_CURRENCY", "USD").upper()
authorized = []


def make_payments():
    for amount in (10000, 2500, 399):
        status, payment = call("POST", "/payments", card(amount, currency=CURRENCY))
        assert status == 201, "a payment of %d gave %s %s" % (amount, status, payment)
        authorized.append(payment)
    status, _ = call("POST", "/payments", card(7700, currency=CURRENCY, token="tok_risky", merchant="m-441"))
    assert status in (202, 402), "a risky payment gave %s" % status


def refund_some():
    status, body = call("POST", "/refunds", {"paymentId": authorized[0]["paymentId"], "amount": 4000})
    assert status == 201, "a partial refund gave %s %s" % (status, body)
    status, body = call("POST", "/refunds", {"paymentId": authorized[1]["paymentId"], "amount": 2500})
    assert status == 201, "a full refund gave %s %s" % (status, body)


def ledger_matches_the_payments():
    status, listing = call("GET", "/payments?currency=" + CURRENCY)
    assert status == 200, "listing the payments gave %s" % status
    expected = sum(p["amount"] - p["refunded"] for p in listing["payments"] if p["status"] == "authorized")
    actual = balance(CURRENCY)
    assert actual == expected, "%s ledger balance %d, payments say %d" % (CURRENCY, actual, expected)


def every_payment_can_be_read_back():
    for payment in authorized:
        status, again = call("GET", "/payments/" + payment["paymentId"])
        assert status == 200 and again["status"] == "authorized", "%s cannot be read back: %s" % (payment["paymentId"], status)


def parked_payments_are_not_in_the_ledger():
    status, listing = call("GET", "/payments?currency=" + CURRENCY)
    posted = sum(p["amount"] for p in listing["payments"] if p["status"] == "authorized")
    refunded = sum(p["refunded"] for p in listing["payments"])
    assert balance(CURRENCY) == posted - refunded, "the ledger includes a payment that was not authorized"


check("%s: payments are made" % CURRENCY, make_payments)
check("%s: refunds are accepted" % CURRENCY, refund_some)
check("%s: the ledger balances against the payments" % CURRENCY, ledger_matches_the_payments)
check("%s: every payment can be read back" % CURRENCY, every_payment_can_be_read_back)
check("%s: parked payments are not in the ledger" % CURRENCY, parked_payments_are_not_in_the_ledger)
finish("reports/reconcile-%s.xml" % CURRENCY.lower(), "reconcile-" + CURRENCY.lower())

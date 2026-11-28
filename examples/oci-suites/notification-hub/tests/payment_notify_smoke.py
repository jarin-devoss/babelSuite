import json
import os
import sys
import urllib.error
import urllib.request
from xml.sax.saxutils import escape

API = os.environ.get("NOTIFICATION_API_URL", "http://notify-api:8080")
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


BROKER = os.environ.get("BROKER_URL", "http://broker-api:8080")
PAYMENTS = os.environ.get("PAYMENTS_URL", "http://payment-api:8080")
SUFFIX = os.environ.get("RUN_SUFFIX", str(os.getpid()))


def token_for(name):
    """Signs in with the identity broker, as a caller of notify-api must."""
    status, answer = call("POST", "/login?provider=oidc", {"email": "%s-%s@babelsuite.test" % (name, SUFFIX), "realm": "realm-0"}, base=BROKER)
    assert status == 200, "the identity broker would not sign %s in: %s %s" % (name, status, answer)
    return answer["token"], "%s-%s@babelsuite.test" % (name, SUFFIX)


def auth(token):
    return {"Authorization": "Bearer " + token}


def send(token, channel, template="transactional", recipient=None, **fields):
    body = {"channel": channel, "template": template, "locale": "en", "recipient": recipient or "user@example.com",
            "vars": {"name": "Ada", "topic": "your order"}}
    body.update(fields)
    return call("POST", "/api/v1/notifications", body, headers=auth(token))

EVENT = os.environ.get("PAYMENT_EVENT_TYPE", "charge.succeeded")
state = {}


def payment(amount=4200, token="tok_visa", currency="USD"):
    status, body = call("POST", "/payments", {"amount": amount, "currency": currency, "cardToken": token, "merchantId": "m-117"}, base=PAYMENTS)
    return status, body


def the_event_a_payment_bears_out_is_announced():
    state["token"], state["email"] = token_for("payment-" + EVENT.replace(".", "-"))
    if EVENT == "charge.failed":
        status, paid = payment(token="tok_risky")
        if status == 402:  # a strict fraud strategy declines instead of parking
            return
        assert status == 202, "setup: a risky card gave %s %s" % (status, paid)
    else:
        status, paid = payment()
        assert status == 201, "setup: payment gave %s %s" % (status, paid)
        if EVENT == "refund.created":
            status, _ = call("POST", "/refunds", {"paymentId": paid["paymentId"], "amount": 1000}, base=PAYMENTS)
            assert status == 201, "setup: refund gave %s" % status
    status, record = call("POST", "/payment-events", {"eventType": EVENT, "paymentId": paid["paymentId"]}, headers=auth(state["token"]))
    if status != 201:
        print("EVENT_DROPPED %s for %s was not announced: %s %s" % (EVENT, paid["paymentId"], status, record))
    assert status == 201, "%s gave %s %s" % (EVENT, status, record)
    assert paid["paymentId"] in record["body"] and str(paid["amount"]) in record["body"], "the announcement does not carry the payment: %r" % record["body"]


def an_event_the_payment_does_not_bear_out_is_dropped():
    status, paid = payment(amount=777, token="tok_risky")
    if status != 202:
        return  # a strict strategy declines it: there is no parked payment to announce
    status, body = call("POST", "/payment-events", {"eventType": "charge.succeeded", "paymentId": paid["paymentId"]}, headers=auth(state["token"]))
    assert status == 409, "announcing a success for a parked payment gave %s %s, want 409" % (status, body)


def an_unknown_payment_is_refused():
    status, body = call("POST", "/payment-events", {"eventType": EVENT, "paymentId": "pay_missing"}, headers=auth(state["token"]))
    assert status == 422, "an event for an unknown payment gave %s %s, want 422" % (status, body)


check(EVENT + ": an event the payment bears out is announced with the payment's data", the_event_a_payment_bears_out_is_announced)
check(EVENT + ": a success for a parked payment is not announced", an_event_the_payment_does_not_bear_out_is_dropped)
check(EVENT + ": an event for an unknown payment is refused", an_unknown_payment_is_refused)
finish("reports/payment-notify-%s.xml" % EVENT.replace(".", "-"), "payment-notify-" + EVENT.replace(".", "-"))

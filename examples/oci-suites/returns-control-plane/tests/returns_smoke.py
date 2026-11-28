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

def a_damaged_item_is_refunded_at_once():
    amount = min(4200, LIMIT)
    status, record = new_return("ord_1001", amount, "damaged")
    assert status == 201 and record["status"] == "approved", "a damaged item gave %s %s" % (status, record)
    assert record["refundCents"] == 4200 and record["quoteId"].startswith("quote_"), "the refund is not the pricing service's quote: %s" % record
    assert record["partnerId"], "the return was not registered with the partner"
    status, again = call("GET", "/returns/" + record["returnId"])
    assert status == 200 and again["status"] == "approved", "the return cannot be read back: %s %s" % (status, again)


def suspicious_activity_goes_to_review():
    status, record = new_return("ord_2042", 15900, "suspicious_activity")
    assert status == 202 and record["status"] == "manual_review" and record["refundCents"] == 0, "suspicious activity gave %s %s" % (status, record)


def no_quote_means_no_return():
    status, body = new_return("ord_3000", 1000, "a-reason-the-pricing-service-has-no-quote-for")
    assert status == 502, "a return the pricing service would not quote gave %s %s, want 502" % (status, body)


def a_refund_above_the_limit_always_needs_a_person():
    # The pricing mock quotes "instant" for a damaged item whatever the amount.
    status, record = new_return("ord_4000", LIMIT + 1, "damaged")
    assert status == 202 and record["status"] == "manual_review", "a refund of %d (limit %d) was issued without review: %s %s" % (LIMIT + 1, LIMIT, status, record)
    assert record["refundCents"] == 0, "money was refunded before review: %s" % record


def invalid_returns_are_refused():
    for body in ({}, {"orderId": "o"}, {"orderId": "o", "amountCents": -1}):
        status, answer = call("POST", "/returns", body)
        assert status == 400, "%s gave %s %s, want 400" % (body, status, answer)


check("a damaged item is refunded at the pricing service's quote", a_damaged_item_is_refunded_at_once)
check("suspicious activity is held for manual review", suspicious_activity_goes_to_review)
check("a return the pricing service will not quote is not created", no_quote_means_no_return)
check("a refund above the limit is never issued without review", a_refund_above_the_limit_always_needs_a_person)
check("an invalid return is refused", invalid_returns_are_refused)
finish("reports/returns-smoke.xml", "returns-smoke")

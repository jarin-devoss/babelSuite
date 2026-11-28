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

CHANNEL = os.environ.get("CHANNEL", "email")
DLQ = os.environ.get("DLQ_ENABLED", "true") == "true"
state = {}


def only_a_signed_in_caller_is_heard():
    status, _ = call("POST", "/api/v1/notifications", {"channel": CHANNEL, "template": "transactional", "recipient": "a@b.test"})
    assert status == 401, "an unauthenticated send gave %s, want 401" % status
    status, _ = call("POST", "/api/v1/notifications", {}, headers=auth("forged.token"))
    assert status == 401, "a forged token gave %s, want 401" % status


def a_send_is_delivered_and_rendered():
    state["token"], state["email"] = token_for("smoke-" + CHANNEL)
    status, record = send(state["token"], CHANNEL)
    assert status == 201 and record["status"] == "sent", "a send gave %s %s" % (status, record)
    assert "Ada" in record["body"] and "your order" in record["body"] and "{{" not in record["body"], "the template was not rendered: %r" % record["body"]
    status, listing = call("GET", "/delivered?channel=" + CHANNEL, headers=auth(state["token"]))
    assert any(r["id"] == record["id"] for r in listing["delivered"]), "the delivery is not in /delivered"
    status, again = call("GET", "/api/v1/notifications/" + record["id"], headers=auth(state["token"]))
    assert status == 200 and again["status"] == "sent", "the record cannot be read back: %s" % status


def a_provider_refusal_is_dead_lettered():
    status, record = send(state["token"], CHANNEL, recipient="fail@example.test")
    if DLQ:
        assert status == 202 and record["status"] == "dead-lettered", "a refused send gave %s %s, want 202 dead-lettered" % (status, record)
        status, listing = call("GET", "/dlq", headers=auth(state["token"]))
        mine = [r for r in listing["dlq"] if r["id"] == record["id"]]
        assert mine and mine[0]["error"] and mine[0]["attempts"] >= 1, "the refused send is not in the DLQ with its error"
        status, listing = call("GET", "/delivered?channel=" + CHANNEL, headers=auth(state["token"]))
        assert not any(r["id"] == record["id"] for r in listing["delivered"]), "a refused send was counted as delivered"
    else:
        assert status == 502, "a refused send with no DLQ gave %s %s, want 502" % (status, record)


def unknown_templates_and_locales_are_refused():
    for template, locale in (("no_such_template", "en"), ("transactional", "xx")):
        status, body = send(state["token"], CHANNEL, template=template, locale=locale)
        assert status == 422, "template %s locale %s gave %s %s, want 422" % (template, locale, status, body)


def a_missing_variable_is_refused():
    status, body = send(state["token"], CHANNEL, vars={"name": "Ada"})
    assert status == 422, "a send without the template's variables gave %s %s, want 422" % (status, body)


check(CHANNEL + ": only a caller the identity broker vouches for is heard", only_a_signed_in_caller_is_heard)
check(CHANNEL + ": a send is rendered, delivered and readable", a_send_is_delivered_and_rendered)
check(CHANNEL + ": a send the provider refuses is dead-lettered, not delivered", a_provider_refusal_is_dead_lettered)
check(CHANNEL + ": an unknown template or locale is refused", unknown_templates_and_locales_are_refused)
check(CHANNEL + ": a missing variable is refused", a_missing_variable_is_refused)
finish("reports/%s-junit.xml" % CHANNEL, "notify-smoke-" + CHANNEL)

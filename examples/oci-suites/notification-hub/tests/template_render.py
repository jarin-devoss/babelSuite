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

LOCALE = os.environ.get("LOCALE", "en")
CATEGORIES = [c for c in os.environ.get("CATEGORIES", "transactional,marketing,alert,digest").split(",") if c]
GREETINGS = {"en": "Hello", "de": "Hallo", "fr": "Bonjour", "ja": "Konnichiwa"}
state = {}


def every_category_renders_in_the_locale():
    state["token"], _ = token_for("render-" + LOCALE)
    for category in CATEGORIES:
        status, record = send(state["token"], "email", template=category, locale=LOCALE)
        assert status == 201, "%s/%s gave %s %s" % (category, LOCALE, status, record)
        assert record["body"].startswith(GREETINGS[LOCALE]), "%s/%s did not render in the locale: %r" % (category, LOCALE, record["body"])
        assert category in record["body"] and "{{" not in record["subject"] + record["body"], "%s/%s left a placeholder: %r" % (category, LOCALE, record)


check("%s: every template category renders in the locale" % LOCALE, every_category_renders_in_the_locale)
finish("reports/render-%s.xml" % LOCALE, "template-render-" + LOCALE)

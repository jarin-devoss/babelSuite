import json
import os
import sys
import urllib.error
import urllib.request
from xml.sax.saxutils import escape

API = os.environ.get("HEALTH_ENDPOINT", "http://payment-api:8080/health")
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


import time


def ready(url, timeout):
    """The /health answer of a service, once it is 200, or the last thing seen."""
    deadline = time.time() + timeout
    last = "no answer"
    while True:
        try:
            with urllib.request.urlopen(url, timeout=5) as reply:
                return reply.status, json.loads(reply.read() or b"{}")
        except urllib.error.HTTPError as err:
            last = "HTTP %d" % err.code
        except Exception as err:
            last = "%s: %s" % (type(err).__name__, err)
        if time.time() >= deadline:
            return 0, last
        time.sleep(2)

# Asks every imported service, by its name, whether it is ready, and reports one
# case per suite. A suite in the list that answers nothing fails the whole run.
ENDPOINTS = dict(pair.split("=", 1) for pair in os.environ.get("ENDPOINTS", "").split(",") if "=" in pair)
TIMEOUT = int(os.environ.get("TIMEOUT_SECONDS", "120"))
assert ENDPOINTS, "no ENDPOINTS were given"


def check_suite(name, url):
    def run():
        status, body = ready(url, TIMEOUT)
        if status != 200:
            print("HEALTH_CHECK_FAILED %s: %s" % (name, body))
        assert status == 200 and body.get("status") == "ok", "%s: %s" % (name, body)
    return run


for name, url in ENDPOINTS.items():
    check("%s is ready for the composed environment" % name, check_suite(name, url))
finish("reports/composite-summary.xml", "composite-health-summary")

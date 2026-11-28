import json
import os
import sys
import urllib.error
import urllib.request
from xml.sax.saxutils import escape

API = os.environ.get("API_URL", "http://dispatcher:8080")
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


def route(name, vehicle="vh-11", zone="depot-north", region="eu-west"):
    return {"routeId": name, "vehicleId": vehicle, "zone": zone, "region": region}

REGION = os.environ.get("REGION", "eu-west")


def a_route_is_assigned_in_the_region():
    status, record = call("POST", "/routes/assign", route("acceptance-" + REGION, region=REGION))
    assert status == 201 and record["region"] == REGION, "a route in %s gave %s %s" % (REGION, status, record)
    status, again = call("GET", "/routes/acceptance-" + REGION)
    assert status == 200 and again["region"] == REGION, "the route is not stored under %s: %s %s" % (REGION, status, again)


def another_region_is_not_served():
    status, body = call("POST", "/routes/assign", route("acceptance-other", region="not-" + REGION))
    assert status == 400, "an unknown region gave %s %s, want 400" % (status, body)


check(REGION + ": a route is assigned and stored in the region", a_route_is_assigned_in_the_region)
check(REGION + ": a region that is not configured is refused", another_region_is_not_served)
finish("reports/acceptance-%s.xml" % REGION, "acceptance-" + REGION)

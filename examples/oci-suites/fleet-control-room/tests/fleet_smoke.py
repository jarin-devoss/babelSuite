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

state = {}


def a_route_is_assigned_by_the_planner():
    status, record = call("POST", "/routes/assign", route("smoke-1"))
    assert status == 201, "a route gave %s %s" % (status, record)
    assert record["assignmentId"] == "asg-778" and record["plannerRevision"].startswith("route-planner@"), "the assignment is not the planner's: %s" % record
    status, again = call("GET", "/routes/smoke-1")
    assert status == 200 and again["assignmentId"] == record["assignmentId"], "the assignment cannot be read back: %s %s" % (status, again)


def telemetry_is_accepted():
    status, body = call("POST", "/telemetry/events", {"vehicleId": "vh-11", "battery": 76, "speed": 0})
    assert status == 202 and body.get("accepted") is True, "a telemetry frame gave %s %s" % (status, body)


def the_control_room_reports_what_it_did():
    status, body = call("GET", "/fleet/status")
    assert status == 200 and body["status"] == "dispatching", "the status gave %s %s" % (status, body)
    assert body["assignments"] >= 1 and body["telemetryFrames"] >= 1, "the status does not count the work done: %s" % body


def an_incomplete_route_is_refused():
    for bad in ({}, {"routeId": "x"}, route("x", region="mars")):
        status, body = call("POST", "/routes/assign", bad)
        assert status == 400, "%s gave %s %s, want 400" % (bad, status, body)


check("a route is assigned by the planner and can be read back", a_route_is_assigned_by_the_planner)
check("a telemetry frame is accepted", telemetry_is_accepted)
check("the control room reports the work it did", the_control_room_reports_what_it_did)
check("an incomplete route is refused", an_incomplete_route_is_refused)
finish("reports/fleet-junit.xml", "fleet-smoke")

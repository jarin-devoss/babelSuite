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

def the_planner_having_no_plan_is_a_refusal_not_an_assignment():
    status, body = call("POST", "/routes/assign", route("degraded-1", vehicle="vh-unknown"))
    assert status == 502, "a vehicle the planner has no plan for gave %s %s, want 502" % (status, body)
    assert call("GET", "/routes/degraded-1")[0] == 404, "a route the planner refused was stored"


def a_telemetry_frame_the_service_refuses_is_reported():
    status, body = call("POST", "/telemetry/events", {"vehicleId": "vh-bad", "battery": -1, "speed": 0})
    assert status == 502, "a refused telemetry frame gave %s %s, want 502" % (status, body)


def the_control_room_keeps_dispatching():
    status, body = call("POST", "/routes/assign", route("degraded-2"))
    assert status == 201, "after the refusals a good route gave %s %s" % (status, body)


check("a vehicle the planner has no plan for is refused", the_planner_having_no_plan_is_a_refusal_not_an_assignment)
check("a telemetry frame the service refuses is reported", a_telemetry_frame_the_service_refuses_is_reported)
check("the control room keeps dispatching after both", the_control_room_keeps_dispatching)
finish("reports/route-degradation.xml", "route-degradation")

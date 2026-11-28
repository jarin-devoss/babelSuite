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

RESTRICTED = "airport-apron"
REGIONS = [r for r in os.environ.get("REGIONS", "eu-west").split(",") if r]


def restricted_zones_are_never_routed():
    for region in REGIONS:
        status, body = call("POST", "/routes/assign", route("fenced-" + region, zone=RESTRICTED, region=region))
        if status == 201:
            print("BOUNDARY_VIOLATION a route went into %s in %s" % (RESTRICTED, region))
        assert status == 403, "a route into %s in %s gave %s %s, want 403" % (RESTRICTED, region, status, body)
        status, _ = call("GET", "/routes/fenced-" + region)
        assert status == 404, "a refused route was stored in %s" % region


def the_planner_is_not_asked_for_a_refused_route():
    before = call("GET", "/fleet/status")[1]["assignments"]
    call("POST", "/routes/assign", route("fenced-again", zone=RESTRICTED))
    assert call("GET", "/fleet/status")[1]["assignments"] == before, "a refused route was counted as an assignment"


def an_open_zone_is_still_served():
    status, body = call("POST", "/routes/assign", route("open-1", zone="depot-north"))
    assert status == 201, "a route into an open zone gave %s %s" % (status, body)


check("no route enters the restricted zone, in any region", restricted_zones_are_never_routed)
check("a refused route does not reach the planner", the_planner_is_not_asked_for_a_refused_route)
check("a route into an open zone is still assigned", an_open_zone_is_still_served)
finish("reports/geo-fence-audit.xml", "geo-fence-audit")

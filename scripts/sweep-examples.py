#!/usr/bin/env python3
"""Run every example suite against a running backend and check how each one ends.

Run it nightly, or by hand after a change to the runtime or to an example:

    ADMIN_EMAIL=... ADMIN_PASSWORD=... python scripts/sweep-examples.py
    python scripts/sweep-examples.py --suite payment-suite --suite electronics-lab
    python scripts/sweep-examples.py --break          # also break each service's named line

It needs Docker, the local registry (see push-to-zot.py) and the backend. Each
suite is pushed first, so the run is of what is in the working tree.

What it asserts:

  * every example ends Healthy under its default profile, except the two profiles
    that exist to fail (electronics-lab under broken.yaml, kafka-consumer-health
    under lagging.yaml), which must end Failed;
  * no step is skipped, so nothing hides behind a failure;
  * with --break, patching the one line each flagship service names makes its suite
    end Failed (a test that still passes with the service broken tests nothing).
"""

import argparse
import http.cookiejar
import json
import os
import pathlib
import subprocess
import sys
import time
import urllib.error
import urllib.request

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
SUITES_DIR = REPO_ROOT / "examples" / "oci-suites"
TERMINAL = {"healthy", "failed", "cancelled", "error"}

# suite id -> [(profile, expected final status)]. A suite not listed here is run
# under local.yaml and expected Healthy.
EXPECTED = {
    "electronics-lab": [("local.yaml", "Healthy"), ("strict.yaml", "Healthy"), ("broken.yaml", "Failed")],
    "kafka-consumer-health": [("local.yaml", "Healthy"), ("lagging.yaml", "Failed")],
}

# The one line each flagship service names as the rule its suite protects. The
# replacement turns the rule off; the suite must then fail.
# Steps that are skipped on a good run on purpose: they run only on a failure
# (on_failure=), or come after one that does.
EXPECTED_SKIPS = {"returns-control-plane": {"rollback-returns", "done"}}

# Suites that run a real model (a multi-gigabyte pull): left out unless --live.
LIVE = {"ai-skill-eval-live", "llm-judge-live"}

BREAKS = {
    "payment-suite": ("tasks/payment_api.py", "if code == AUTHORIZED:  # the rule", "if True:  # the rule"),
    "identity-broker": ("tasks/broker_api.py", "        if not hmac.compare_digest(signature, expected):", "        if False:"),
    "notification-hub": ("tasks/notify_api.py", "    with lock:\n        dead.append(dict(record, error=error, attempts=attempts))", "    return"),
    "returns-control-plane": ("tasks/refund_worker.py", "    if amount > LIMIT:  # the rule", "    if False:  # the rule"),
    "soap-claims-hub": ("tasks/claims_bridge.py", '    if "Fault" in parsed:  # the rule', "    if False:  # the rule"),
    "fleet-control-room": ("tasks/dispatcher.py", "    if not allowed:  # the rule", "    if False:  # the rule"),
    "storefront-browser-lab": ("tasks/storefront.py", "    if status == 409:  # the rule", "    if False:  # the rule"),
}

BASE = os.environ.get("BABELSUITE_URL", "http://localhost:8090")
jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
token = None


def call(method, path, body=None, timeout=60):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(BASE + path, data=data, method=method, headers={"Content-Type": "application/json"})
    if token:
        request.add_header("Authorization", "Bearer " + token)
    else:
        for cookie in jar:
            if cookie.name == "csrf_token":
                request.add_header("X-CSRF-Token", cookie.value)
    try:
        with opener.open(request, timeout=timeout) as reply:
            raw = reply.read()
            return reply.status, (json.loads(raw) if raw[:1] in (b"{", b"[") else raw.decode(errors="replace"))
    except urllib.error.HTTPError as err:
        raw = err.read()
        try:
            return err.code, json.loads(raw)
        except ValueError:
            return err.code, raw.decode(errors="replace")


def sign_in():
    global token
    email, password = os.environ.get("ADMIN_EMAIL", ""), os.environ.get("ADMIN_PASSWORD", "")
    if not email or not password:
        sys.exit("set ADMIN_EMAIL and ADMIN_PASSWORD")
    call("GET", "/api/v1/auth/config")
    status, reply = call("POST", "/api/v1/auth/sign-in", {"email": email, "password": password})
    if status != 200:
        sys.exit("sign-in failed: %s %s" % (status, reply))
    token = reply.get("token") or reply.get("accessToken") or reply.get("access_token")


def push(suite):
    result = subprocess.run([sys.executable, str(REPO_ROOT / "scripts" / "push-to-zot.py"), "--suite", suite, "--sync", BASE],
                            cwd=REPO_ROOT, capture_output=True, text=True)
    if result.returncode != 0:
        sys.exit("push of %s failed:\n%s%s" % (suite, result.stdout, result.stderr))


def run(suite, profile, timeout):
    """Launches a run and waits for it. Returns (status, the steps that were skipped, the execution id)."""
    status, reply = call("POST", "/api/v1/executions", {"suiteId": suite, "profile": profile})
    if status not in (200, 201, 202):
        return "launch failed: %s %s" % (status, reply), [], ""
    execution_id = reply.get("id") or reply.get("executionId")
    deadline = time.time() + timeout
    record = reply
    while time.time() < deadline:
        _, record = call("GET", "/api/v1/executions/%s" % execution_id)
        if isinstance(record, dict) and str(record.get("status", "")).lower() in TERMINAL:
            break
        time.sleep(3)
    else:
        return "timed out", [], execution_id
    skipped = sorted({event.get("id", "")[:-len("-skipped")] for event in record.get("events", []) if str(event.get("id", "")).endswith("-skipped")})
    return record.get("status", "?"), skipped, execution_id


def break_one(suite, timeout):
    rel, old, new = BREAKS[suite]
    path = SUITES_DIR / suite / rel
    original = path.read_bytes().decode("utf-8")
    text = original.replace("\r\n", "\n")
    if old not in text:
        return "the named line is not in %s any more" % rel
    broken = text.replace(old, new)
    if "\r\n" in original:
        broken = broken.replace("\n", "\r\n")
    try:
        path.write_bytes(broken.encode("utf-8"))
        push(suite)
        status, _, _ = run(suite, "local.yaml", timeout)
    finally:
        path.write_bytes(original.encode("utf-8"))
        push(suite)
    return None if status == "Failed" else "ended %s with its rule broken, want Failed" % status


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--suite", action="append", help="only these suites")
    parser.add_argument("--live", action="store_true", help="include the suites that run a real model")
    parser.add_argument("--break", dest="break_rules", action="store_true", help="also break each flagship service's named line")
    parser.add_argument("--timeout", type=int, default=600, help="seconds a run may take")
    args = parser.parse_args()

    sign_in()
    suites = sorted(p.name for p in SUITES_DIR.iterdir() if p.is_dir())
    if args.suite:
        suites = [s for s in suites if s in args.suite]
    elif not args.live:
        suites = [s for s in suites if s not in LIVE]

    problems = []
    print("%-26s %-14s %-9s %-9s %s" % ("suite", "profile", "ended", "want", "execution"))
    for suite in suites:
        push(suite)
        for profile, want in EXPECTED.get(suite, [("local.yaml", "Healthy")]):
            ended, skipped, execution_id = run(suite, profile, args.timeout)
            verdict = "" if ended == want else "   <-- WRONG"
            print("%-26s %-14s %-9s %-9s %s%s" % (suite, profile, ended, want, execution_id, verdict))
            if ended != want:
                problems.append("%s under %s ended %s, want %s" % (suite, profile, ended, want))
            unexpected = [s for s in skipped if s not in EXPECTED_SKIPS.get(suite, set())]
            if want == "Healthy" and unexpected:
                problems.append("%s under %s skipped %s: a step is hiding behind another" % (suite, profile, ", ".join(unexpected)))

    if args.break_rules:
        print("\nbreaking each service's named line:")
        for suite in suites:
            if suite not in BREAKS:
                continue
            problem = break_one(suite, args.timeout)
            print("  %-26s %s" % (suite, problem or "went red, as it should"))
            if problem:
                problems.append("%s: %s" % (suite, problem))

    if problems:
        print("\n%d problem(s):" % len(problems))
        for problem in problems:
            print("  - " + problem)
        return 1
    print("\nevery example ended as expected")
    return 0


if __name__ == "__main__":
    sys.exit(main())

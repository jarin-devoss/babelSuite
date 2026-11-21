#!/usr/bin/env python3
"""Run the load-testing proof suites against a throw-away control plane.

Each suite in e2e/load/suites has a file of the same name in e2e/load/expect that
says what a correct run looks like. The runner starts the same throw-away stack the Playwright suite
uses (frontend/e2e/stack/backend.mjs, pointed at these suites), launches every
suite through the API, and compares the outcome with its expectations:

    status                      the run's final status (Healthy or Failed)
    log_contains                strings that must appear in the run's log
    log_not_contains            strings that must not
    count_equals_reported       the requests a traffic step reports equal the
                                count its target service read back
    count_equals_reported_sum   the same, summed over a scalability run's probes

Needs Docker, Go, Node and Python with pyyaml, plus MongoDB and the Zot registry
from demo/docker-compose.yml. The control plane listens on :8090.

    python e2e/load/run.py                  # every suite
    python e2e/load/run.py --suite load-count --suite load-ghost
"""
import argparse
import http.cookiejar
import json
import os
import pathlib
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request

import yaml

REPO = pathlib.Path(__file__).resolve().parents[2]
SUITES = REPO / "e2e" / "load" / "suites"
EXPECT = REPO / "e2e" / "load" / "expect"
FRONTEND = REPO / "frontend"
SESSION = FRONTEND / "e2e" / ".auth" / "session.json"
LOG = FRONTEND / "e2e" / ".auth" / "load-stack.log"
TERMINAL = {"Healthy", "Failed"}


class Api:
    def __init__(self, base, email, password):
        self.base = base
        jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
        self.opener.open(base + "/api/v1/auth/config").read()
        csrf = next(c.value for c in jar if c.name == "csrf_token")
        request = urllib.request.Request(
            base + "/api/v1/auth/sign-in",
            json.dumps({"email": email, "password": password}).encode(),
            {"Content-Type": "application/json", "X-CSRF-Token": csrf},
        )
        self.token = json.load(self.opener.open(request))["token"]

    def call(self, method, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(
            self.base + path, data=data, method=method,
            headers={"Authorization": "Bearer " + self.token, "Content-Type": "application/json"},
        )
        try:
            with self.opener.open(request, timeout=60) as response:
                raw = response.read()
                return response.status, (json.loads(raw) if raw[:1] in (b"{", b"[") else raw.decode(errors="replace"))
        except urllib.error.HTTPError as error:
            raw = error.read()
            try:
                return error.code, json.loads(raw)
            except ValueError:
                return error.code, raw.decode(errors="replace")


def start_stack():
    SESSION.unlink(missing_ok=True)
    env = dict(os.environ, E2E_SUITES_DIR=str(SUITES), E2E_REGISTRY_SCOPE="e2e-load")
    # The stack's output goes to a file: a pipe nobody reads fills up and stops the server.
    LOG.parent.mkdir(parents=True, exist_ok=True)
    log = open(LOG, "w", encoding="utf-8")
    process = subprocess.Popen(["node", "e2e/stack/backend.mjs"], cwd=FRONTEND, env=env,
                               stdout=log, stderr=subprocess.STDOUT, text=True)
    deadline = time.time() + 300
    while time.time() < deadline:
        if process.poll() is not None:
            sys.exit(f"the stack exited early; see {LOG}")
        if SESSION.exists():
            session = json.loads(SESSION.read_text())
            try:
                urllib.request.urlopen(session["apiUrl"] + "/readyz", timeout=2)
                return process, session
            except OSError:
                pass
        time.sleep(1)
    process.kill()
    sys.exit("the control plane did not become ready")


def stop_stack(process, session, run_ids, api):
    try:
        for environment in (api.call("GET", "/api/v1/sandboxes")[1] if api else []) or []:
            if isinstance(environment, dict) and environment.get("runId") in run_ids:
                api.call("POST", f"/api/v1/sandboxes/{environment['sandboxId']}/reap")
    except OSError:
        pass  # the control plane is stopped below either way
    if os.name == "nt":
        subprocess.run(["taskkill", "/T", "/F", "/PID", str(process.pid)], capture_output=True)
        subprocess.run(["taskkill", "/F", "/IM", "babelsuite-server.exe"], capture_output=True)
    else:
        process.terminate()
    subprocess.run(["docker", "exec", "babelsuite-demo-mongo", "mongosh", "--quiet", "--eval",
                    f"db.getSiblingDB('{session['mongoDb']}').dropDatabase()"], capture_output=True)
    sidecars = subprocess.run(["docker", "ps", "-aq", "--filter", "name=babel-sidecar-load-"],
                              capture_output=True, text=True).stdout.split()
    if sidecars:
        subprocess.run(["docker", "rm", "-f", *sidecars], capture_output=True)


def run_suite(api, suite_id, timeout):
    code, body = api.call("POST", "/api/v1/executions", {"suiteId": suite_id, "profile": "local.yaml"})
    if code not in (200, 201, 202):
        return "Failed", [f"launch refused: {code} {body}"], None
    run_id = body.get("id")
    deadline = time.time() + timeout
    record = body
    while time.time() < deadline:
        _, record = api.call("GET", f"/api/v1/executions/{run_id}")
        if isinstance(record, dict) and record.get("status") in TERMINAL:
            break
        time.sleep(2)
    _, logs = api.call("GET", f"/api/v1/executions/{run_id}/logs/snapshot")
    items = logs if isinstance(logs, list) else (logs.get("lines") or logs.get("logs") or [])
    lines = [(item.get("text") or "") if isinstance(item, dict) else str(item) for item in items]
    return (record.get("status") if isinstance(record, dict) else "?"), lines, run_id


def check(expect, status, lines):
    problems = []
    text = "\n".join(lines)
    if "status" in expect and status != expect["status"]:
        problems.append(f"status is {status}, want {expect['status']}")
    for want in expect.get("log_contains", []):
        if want not in text:
            problems.append(f"log lacks {want!r}")
    for unwanted in expect.get("log_not_contains", []):
        if unwanted in text:
            problems.append(f"log has {unwanted!r}")

    counted = re.findall(r'COUNT-RESULT \{"count": (\d+)', text)
    if expect.get("count_equals_reported"):
        reported = re.findall(r"Traffic run completed with (\d+) requests", text)
        if not reported or not counted:
            problems.append(f"need both figures, reported={reported} counted={counted}")
        elif int(reported[-1]) != int(counted[-1]) or int(reported[-1]) == 0:
            problems.append(f"the step reported {reported[-1]} requests but the service counted {counted[-1]}")
    if expect.get("count_equals_reported_sum"):
        probes = [int(n) for n in re.findall(r"Probe \d+ result: users=\d+ requests=(\d+)", text)]
        if not probes or not counted:
            problems.append(f"need both figures, probes={probes} counted={counted}")
        elif sum(probes) != int(counted[-1]) or sum(probes) == 0:
            problems.append(f"the probes reported {sum(probes)} requests but the service counted {counted[-1]}")
    return problems


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--suite", action="append", help="run only these suites")
    parser.add_argument("--timeout", type=int, default=240, help="seconds to wait for one run")
    args = parser.parse_args()

    names = sorted(p.stem for p in EXPECT.glob("*.yaml"))
    if args.suite:
        names = [n for n in names if n in args.suite]

    process, session = start_stack()
    api, run_ids, failed = None, set(), []
    try:
        api = Api(session["apiUrl"], session["email"], session["password"])
        for name in names:
            expect = yaml.safe_load((EXPECT / f"{name}.yaml").read_text())
            started = time.time()
            status, lines, run_id = run_suite(api, name, args.timeout)
            if run_id:
                run_ids.add(run_id)
            problems = check(expect, status, lines)
            print(f"{'PASS' if not problems else 'FAIL'}  {name:<20} {status:<8} {time.time() - started:5.0f}s")
            for problem in problems:
                print(f"        - {problem}")
            if problems:
                failed.append(name)
                print("        last log lines:")
                for line in lines[-12:]:
                    print(f"          {line[:700]}")
    finally:
        stop_stack(process, session, run_ids, api)

    print(f"\n{len(names) - len(failed)} of {len(names)} proof suites behaved as expected")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

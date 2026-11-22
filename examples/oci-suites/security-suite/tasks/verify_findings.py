"""Aggregates the reports of every security probe in this suite.

Each security step exports a JSON report into the execution's workspace
(findings/<step>.json). This step reads them all and exits non-zero when:

  - a report is missing, because a probe that wrote nothing checked nothing;
  - a probe could not determine some of its checks (unknown), or ran none;
  - a finding is at or above the severity gate.

so the suite has one place that decides pass or fail. The gate is SEVERITY_GATE
(low, medium, high or critical), high by default.
"""
import json
import os
import pathlib

RANKS = {"info": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}
PROBES = ["probe", "headers", "verbs", "graphql", "cors", "fuzz", "auth", "flood"]

gate = os.environ.get("SEVERITY_GATE", "high").strip().lower()
if gate not in RANKS:
    raise SystemExit(f"verify: SEVERITY_GATE {gate!r} is not one of {', '.join(RANKS)}")

workspace = pathlib.Path(os.environ.get("BABELSUITE_WORKSPACE_DIR", "."))
print(f"verify: severity gate = {gate}, reports in {workspace / 'findings'}")

problems = []
for probe in PROBES:
    path = workspace / "findings" / f"{probe}.json"
    if not path.is_file():
        problems.append(f"{probe}: no report at {path}")
        continue
    report = json.loads(path.read_text())

    checked = report.get("passed", 0) + report.get("failed", 0)
    if checked == 0:
        problems.append(f"{probe}: no check ran against {report.get('target')}")
    if report.get("unknown", 0) > 0:
        problems.append(f"{probe}: {report['unknown']} check(s) could not be determined: {report.get('first_error')}")

    blocking = [f for f in report.get("findings", []) if RANKS.get(str(f.get("severity")).lower(), 2) >= RANKS[gate]]
    for finding in blocking:
        problems.append(f"{probe}: {finding['severity']} {finding['label']} {finding['method']} {finding['path']} — {finding['detail']}")

    print(f"verify: {probe}: {report.get('passed', 0)} passed, {report.get('failed', 0)} failed, "
          f"{report.get('unknown', 0)} unknown, {len(blocking)} at or above {gate}")

if problems:
    for item in problems:
        print(f"verify: BLOCKING {item}")
    raise SystemExit(1)

print(f"verify: {len(PROBES)} probes clean at or below the {gate} gate")

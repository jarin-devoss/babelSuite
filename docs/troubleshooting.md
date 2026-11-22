---
title: Troubleshooting
---

# Troubleshooting

[Back to index](index.md)

---

## Control Plane Won't Start

### `JWT_SECRET must be set to a secure random value`

The server requires a non-empty, non-default `JWT_SECRET`. Generate one and set it:

```bash
export JWT_SECRET=$(openssl rand -hex 32)
```

### `platform settings: ...` on startup

The control plane cannot read `configuration.yaml` at the configured path. Check:

1. The file exists at the path set by `PLATFORM_SETTINGS_FILE` (default: `configuration.yaml` in the working directory).
2. The process has read permission.
3. The YAML is valid — run `cat configuration.yaml | python3 -c "import sys,yaml; yaml.safe_load(sys.stdin)"` to validate.

### MongoDB connection refused

```
store: server selection error: server 127.0.0.1:27017 ... connection refused
```

MongoDB is not running or `MONGO_URI` is wrong. Check:

```bash
mongosh "$MONGO_URI" --eval "db.adminCommand({ping:1})"
```

---

## Readiness Probe Failing (`/readyz` returns 503)

Check which subsystem is unhealthy:

```bash
curl -s http://localhost:8090/readyz | jq .
```

| Subsystem | Common cause |
|-----------|-------------|
| `database` | MongoDB unreachable |
| `platform` | `configuration.yaml` missing or unreadable |
| `profiles` | `babelsuite-profiles.yaml` unreadable |
| `agents` | No agents registered (optional — only required when agent backends are configured) |

---

## Execution Stuck in `pending`

1. **No available backend** — check that at least one backend is healthy: `curl /readyz/agents`.
2. **Agent not heartbeating** — the agent process may have crashed. Check agent logs and restart it.
3. **Suite not found in catalog** — the OCI registry may be unreachable, or the suite was never pushed to it. Run `python scripts/push-to-zot.py` and restart the backend, then confirm with `GET /api/v1/suites/<id>`.

---

## Step Fails Immediately With `context canceled`

The execution was cancelled (e.g. by a `reap` call or a timeout). Check:

1. Whether a suite-level timeout (`timeout:` field) is set too aggressively.
2. Whether the step has a dependency that failed, causing the execution engine to cancel downstream steps.

---

## A Traffic Step Fails

A `traffic.*` step sends real requests from the suite's APISIX sidecar, so a failure names what went wrong. Match the message in the step's log:

| Message | What it means and what to do |
|---------|------------------------------|
| `targets host "x", which is not a service in this suite` | The target is a bare name that no node of the suite provides. Use the name of a `service.run` node, the suite's mock host (`https://<suite>.mock.internal`), or a fully-qualified host. |
| `service "x" is not reachable` | The service has no address on the run's network. Order the traffic step `after=` the service and check that its container started. |
| `target ... did not answer a single request successfully` | Nothing at the target answered with a success. The log lists the error categories (`connect`, `timeout`, `http_4xx:404`); check that the target is listening on the port the URL names. |
| `no requests were sent to ...` | The run finished without a request. Check the plan's stages, users and waits. |
| `http_4xx:404` in the failure | A request got a 404. Fix the path, or declare the status expected with `checks=[traffic.threshold("status", "==", 404)]`. |
| `traffic.dropped ... the target could not keep up` | An open-model run could not start every arrival because too many requests were in flight. Lower `rps=` or `arrival_rate=`. |
| `below the min_users=N the step requires` | A scalability run broke before reaching `min_users=`. The log shows the probe that failed. |
| `the suite's APISIX sidecar is not running, so no load can be generated` | The traffic cannon runs in the sidecar. Run the suite on the local Docker backend. |
| `load against a service is supported only on the local Docker backend` | The target is a service of a Kubernetes backend. Target the suite's mock host or a fully-qualified host instead. |

---

## A Security Step Fails

A `security.*` step scans a target from the suite's APISIX sidecar and fails with a message that says why. Match it in the step's log:

| Message | What it means and what to do |
|---------|------------------------------|
| `N finding(s) at or above low severity` | The target failed checks. Each one is logged as `Finding [label] METHOD path ...`. Fix the target, or raise `fail_on=` (`medium`, `high`, `critical`) to let lower findings through. |
| `no check reached <target>: ...` | Nothing at the target answered. The reason follows (the first error the scanner hit). Check the address, the port and that the service started. |
| `N of M checks could not be determined: ...` | Some requests got no answer, so those checks neither passed nor failed. Check the target stayed up during the scan. |
| `needs target=: the suite has no mock surface to scan by default` | The suite has no `mock.api` and the step names no target. Add `target="http://<service>:<port>"`. |
| `targets host "x", which is not a service in this suite` | The target is a bare name no node provides. Use the name of a `service.run` node, a mock host, or a fully-qualified host. |
| `unknown argument "x"; allowed: ...` | The mode does not take that key (for example `technique=` on `security.headers`). |
| `scanning the suite's own mock` (a warning) | The step has no `target=`, so it measures the gateway and the mock, not a service of yours. Add `target=` to scan the service. |
| `The report was not written to the execution workspace` (a warning) | A later task will not find the exported report. The log says whether the backend does not give the control plane write access to the workspace or the path was rejected. |

---

## An Execution Shows `Interrupted`

The control plane that was running it stopped (a crash, a kill, a restart) before it finished, and nothing resumes a run. The execution's events say when and which step was running. Start the suite again. If it happens without a restart, look for the control plane being killed (an out-of-memory kill, a Kubernetes pod eviction) or for two control planes sharing one store and one Docker host.

## A Stateful Mock Remembers Another Run

| Symptom | What it means and what to do |
|---------|------------------------------|
| The mock's answer has the other run's counts, and the response header `X-Babelsuite-State-Scope` is `suite` | The request could not be tied to a run, so it used the state every run of the suite shares. Call the mock through `$BABELSUITE_GATEWAY_URL`, or send `X-Babelsuite-Execution: $BABELSUITE_EXECUTION_ID`. A Kubernetes or remote-agent run always shares. |
| `X-Babelsuite-State-Scope` is `execution` and the state still looks wrong | `reset_mocks=` clears this run's state only; check that the step that sets the state ran before the one that reads it. |

## A Load Step Waits Before It Starts

`Waiting for <run>/<step>, which is generating load on this suite's sidecar`: a `traffic.*` or `security.flood` step is already running in the same suite and profile's sidecar, and two at once would measure each other. The step starts when the other ends, and logs how long it waited. Give the suite's runs different profiles to give them different sidecars.

## An Agent Stops With `agent id is in use`

Another agent on a different host registered with this `AGENT_ID` and is still sending heartbeats. Give each agent its own id, stop the other one, or wait 30 seconds after it stopped. A registration with `"force": true` takes the id over on purpose.

## A Plugin Step Fails

A plugin step fails with the plugin's own reason. Match the message in the step's log:

| Message | What it means and what to do |
|---------|------------------------------|
| `plugin X config: <field>: ...` | The step's config does not satisfy the plugin's schema (a missing required setting, a wrong type, a value out of range). Fix the call; nothing was sent to the plugin. |
| `plugin X: the reply has no boolean "passed" field` | The plugin replied without a verdict, or with one that is not a boolean. A plugin must reply `{"passed": true|false, ...}`. See the [response contract](plugins.md#plugin-response-contract). |
| `plugin X: <message> (HTTP 500)` | The plugin could not run its check and said why: a target it could not reach, an endpoint its server does not serve, an unexpected response shape. The message names the URL and status. |
| `plugin X: N finding(s) exceeded thresholds` | The plugin found blocking (`critical`) findings. They are listed above this line. A finding at `warn` level does not block. |
| `unknown op Y; supported: ...` | The operation is not one the plugin has. A wrapper function's name is the operation. |
| `this REST proxy does not serve consumer-group lag` / `... partition offsets` | `consumer-lag` and `dlq-inspector` need the Confluent REST Proxy (v3 lag, v2 offsets). Redpanda's HTTP Proxy serves neither. |
| `plugin X: the suite's APISIX sidecar is not running` | Plugins run in the suite's sidecar. Run the suite on the local Docker backend. |

---

## A Test Step Fails On Its Report

A `test.run` step whose `exports=` declare a `junit` or `ctrf` report is checked against that report after its own checks pass, so it can fail although the test command exited 0. The step's log names the report and the reason:

| Message | What it means and what to do |
|---------|------------------------------|
| `report "x" (path) was not produced` | The step wrote no file at that path. Write it relative to the step's working directory, and check that the test runner is told where to write it. |
| `report "x" (path) is empty` | The file exists but has no content. The test runner probably crashed while writing it. |
| `report "x" (path) could not be read: ...` | The file is not valid XML (JUnit) or JSON (CTRF), or has the wrong root element. The execution view shows the file as it was written. |
| `report "x" (path) is too large to read` | A file is over 10 MB. Split the report, or write a summary. |
| `report "x" (path) lists no tests` | The report parsed but holds no test cases. A runner that found no tests is not a pass. |
| `report "x" (path): N of M tests failed` | The report lists failures or errors. Open the artifact to see the counts. |
| `report "x" (path) was not collected: remote agents do not return reports yet` | The step ran on a remote agent. Add `"enforce": False` to the export. |

To show a report without letting it fail the step, add `"enforce": False` to its entry in `exports=`. See [Artifact Exports](runtime-library.md#artifact-exports).

---

## CLI: `error: no saved session`

Run `babelctl login` first. If you've set `--server` but not logged in, the token won't exist.

## CLI: `error: connection refused`

The `--server` URL is wrong or the control plane isn't running. Verify:

```bash
curl http://localhost:8090/healthz
```

## CLI: `error: unauthorized`

The session token has expired. Run `babelctl login` again to refresh it.

---

## OTel / Traces Missing

If traces aren't appearing in your collector:

1. Check `OTEL_EXPORTER_OTLP_ENDPOINT` is set and the collector is reachable.
2. Verify `/readyz/telemetry` — if it returns `disabled`, no endpoint is configured.
3. For self-signed TLS: set `OTEL_EXPORTER_OTLP_INSECURE=true` or mount the CA bundle.

---

## See Also

- [Operations](operations.md) — health endpoints, middleware, TLS setup
- [Configuration](configuration.md) — all environment variables
- [Agents](agents.md) — worker process lifecycle

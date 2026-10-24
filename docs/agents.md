---
title: Agents
---

# Agents

[Back to index](index.md)

Remote agents are worker processes that execute suite steps outside the control plane. You run one where you want work to happen — on a different host, inside a Kubernetes cluster, or in an isolated network — and register it with the control plane. From that point on, any suite launched with that backend routes its steps to that worker.

---

## How Agents Work

```
Agent starts → registers with control plane → sends heartbeats → polls for work
                                                                       │
                                                         control plane assigns a step
                                                                       │
                                                         agent claims it, runs it,
                                                         streams logs and state back,
                                                         then marks it complete
```

1. **Register** — the agent calls `POST /api/v1/agents/register` with its identity, capabilities and `capacity` (how many steps it runs at once).
2. **Heartbeat** — the agent sends periodic heartbeats so the control plane knows it is alive.
3. **Claim** — each of the agent's `AGENT_MAX_JOBS` claim loops polls `POST /api/v1/agent-control/claims/next`. When a step is waiting, the control plane returns a `StepRequest`.
4. **Execute** — the agent runs the step and streams log lines and state transitions back continuously.
5. **Complete** — the agent calls `POST /api/v1/agent-control/jobs/:id/complete` with the final result.

If an agent stops sending heartbeats, the control plane marks it offline. In-progress steps from a disconnected agent are left in their last-known state.

---

## Starting a Remote Agent

```bash
cd backend
go run ./cmd/agent
```

The worker listens on port `8091` by default. Set `AGENT_SHARED_SECRET` on both the control plane and the agent to authenticate registrations.

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENT_ID` | hostname | Identity; must match the `agentId` of the `remote-agent` entry in platform settings, because claims are matched to that backend by id |
| `AGENT_CONTROL_PLANE_URL` | *(empty)* | Control plane base URL; without it the agent only serves its own HTTP API |
| `AGENT_MAX_JOBS` | `1` | Steps this agent runs at once. It is sent with the registration, and the agent runs that many claim loops |
| `AGENT_POLL_INTERVAL` | `1s` | Wait between claims when no step is pending |
| `AGENT_HEARTBEAT_INTERVAL` | `15s` | Heartbeat period |

---

## How Steps Are Scheduled

Which backends a run may land on is set by the platform's deployment mode — `local`, `remote` or `hybrid`, under Settings → General; see [`mode`](configuration.md#mode). Remote agents and Kubernetes backends take work only in `remote` or `hybrid` mode.

The control plane has no worker pool of its own. Every step is queued under the backend that runs it, and starts as soon as its dependencies are done and **that backend** has a free slot: the server only queues, and capacity is what the backends declare.

| Backend | Capacity |
|---------|----------|
| Local Docker | `BABELSUITE_LOCAL_MAX_STEPS`, default one step per CPU of the host |
| Kubernetes | Unlimited — the cluster scheduler already holds a pod `Pending` until a node has room |
| Remote agent | The `AGENT_MAX_JOBS` its agent declared at registration; 1 while it is offline, so steps wait for it to return |

An agent registered before capacity existed counts as one slot. Raising capacity means restarting the agent with a higher `AGENT_MAX_JOBS`, rather than reconfiguring the control plane.

Each remote-agent backend is served by **one** agent, whose `AGENT_ID` is the backend's `agentId`. Steps are dispatched one at a time, and the steps of a run find each other by container name on the agent's own Docker network, so all of a run must land on one host: a second agent started under the same id would replace the first's registration and could take half a run's steps to a host where its services do not exist. To add a host, add another remote-agent backend with its own id, and route to it with routing tags or by choosing it at launch.

A step sent to an agent carries a lease of at least one minute, which the agent renews at half that interval. A renewal that fails is retried; only when the agent has gone a whole lease without renewing does the control plane fail the step, and the agent stop it. A service an agent starts runs until the execution ends — the control plane then queues a release job, and the agent removes the execution's containers and network from its host. An agent that is stopped does the same for every execution it still holds before it exits, since no agent would be left to claim their release jobs. A step whose agent goes silent for a whole lease fails with *the agent stopped renewing the step's lease*, naming the agent.

Job ids are `<execution>:<step id>`, and a step from a composed suite carries its alias path, as in `run-1:identity-broker/pre-hook/db` — clients put the id in `/jobs/{jobId}/…` URLs path-escaped (`%2F` for each `/`).

---

## Control Plane Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/agents` | List registered agents and their status |
| `POST` | `/api/v1/agents/register` | Register a new agent |
| `POST` | `/api/v1/agents/{agentId}/heartbeat` | Refresh the agent's last-seen timestamp |
| `DELETE` | `/api/v1/agents/{agentId}` | Deregister an agent |
| `POST` | `/api/v1/agent-control/claims/next` | Claim the next available step |
| `POST` | `/api/v1/agent-control/jobs/{jobId}/lease` | Extend the step lease while work is in progress |
| `POST` | `/api/v1/agent-control/jobs/{jobId}/state` | Report a step state transition |
| `POST` | `/api/v1/agent-control/jobs/{jobId}/logs` | Stream log lines back to the control plane |
| `POST` | `/api/v1/agent-control/jobs/{jobId}/complete` | Mark step complete with final status |

---

## Worker Process Endpoints

The agent exposes its own HTTP API on port `8091`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Worker liveness check |
| `GET` | `/api/v1/agent/info` | Agent identity and runtime capabilities |
| `POST` | `/api/v1/agent/run` | Run a step |
| `POST` | `/api/v1/agent/jobs/{jobId}/cancel` | Cancel an in-progress step |
| `POST` | `/api/v1/agent/jobs/{jobId}/cleanup` | Clean up resources left by a step |
| `GET` | `/api/v1/agent/inventory` | The executions this agent's Docker holds, for the Environments page |
| `POST` | `/api/v1/agent/executions/{executionId}/reap` | Remove an execution's containers, network and workspace from this host |

---

## Step Request Payload

When the control plane assigns a step, the `StepRequest` contains everything the worker needs to execute it independently:

| Field | Description |
|-------|-------------|
| Execution and suite identity | Which run this step belongs to |
| Profile and runtime profile | Environment overlays for this run |
| Env vars and request headers | Injected into the step container |
| Backend identity | Which registered backend claimed this step |
| Dependency alias, ref, digest | Which OCI artifact to pull |
| Step index and total count | Position in the execution |
| Node definition | Full step spec — image, command, ports, mounts |

---

## Kubernetes Backend

When `type: kubernetes` is set in platform settings, the control plane runs each step that has an image as a pod in `targetNamespace`. Everything else runs exactly as it does on the Docker backend: `log.*` steps are their message, `plugin`, `security.*` and `traffic.*` steps are executed by the control plane against the suite's APISIX sidecar, and `mock.api` is answered by that sidecar.

### How it works

1. **Per execution**, before the first step, the backend creates a headless Service named `babel-<execution>` and a `ReadWriteOnce` volume claim `babel-<execution>-workspace` (1Gi, default storage class). Both are removed when the execution finishes, together with every pod labelled `babelsuite.execution=<execution>`.
2. Every pod mounts the workspace claim at `$BABELSUITE_WORKSPACE_DIR`, so files pass between steps as they do on Docker. A node-local volume keeps an execution's pods on one node.
3. `task.run` and `test.run` pods run the step's `commands=` or `file=` through `BABELSUITE_SCRIPT`, are watched until the **step container** terminates, and are deleted when it does. Their exit code is evaluated against `expect_*` assertions. They drop every capability and cannot escalate privileges, as on Docker.
4. `service.run(image=...)` pods are **detached**: the step returns once the container is running and has survived a 500 ms crash window, and the pod keeps running for later steps. Image pull back-off, an unschedulable pod, or an immediate exit fail the step at once instead of waiting out the 15-minute start timeout.
5. A service pod's hostname is its network alias and its subdomain the execution's headless Service; every pod of the execution searches that subdomain, so `db:5432` resolves to that execution's `db` only. Kubernetes tries the namespace's own domain before that search entry, so keep the namespace for BabelSuite runs: a Service named `db` there would answer for every execution's `db`.
6. Ports a plugin references on a service (`http://broker:8082`) are port-forwarded to `127.0.0.1` on the control plane host, and the plugin config is rewritten to `host.docker.internal:<port>`, the way Docker-published ports are.
7. Pods run as the image's own user, with the profile's `services.<name>.memory` as their memory limit and no CPU limit, and `imagePullPolicy: IfNotPresent`.
8. Artifact collection uses an `emptyDir` volume shared between the step container and a `busybox` sidecar. After the step container exits, artifacts are read through the pod exec API before the pod is deleted.

!!! note "Reaching the host"
    Steps and the sidecar reach the control plane host as `host.docker.internal`, which Docker Desktop and kind resolve inside pods. `BABELSUITE_GATEWAY_URL` is rewritten to that name for pods. A cluster that does not resolve it cannot reach the sidecar from a step.

### Configuration

```yaml
agents:
  - agentId: k8s-prod
    name: Production cluster
    type: kubernetes
    enabled: true
    kubeconfigPath: /etc/babelsuite/kubeconfig   # omit to use in-cluster config
    targetNamespace: babelsuite-jobs
    serviceAccountToken: ""                       # leave empty for kubeconfig auth
    apisixSidecar:
      image: apache/apisix:3.9.0-debian
      listenPort: 9080
      adminPort: 9180
```

| Field | Description |
|-------|-------------|
| `kubeconfigPath` | Absolute path to a kubeconfig file. Omit when running the control plane inside the target cluster — the in-cluster service account is used automatically. |
| `targetNamespace` | Namespace where step pods are created. The service account needs, in this namespace: `create`/`get`/`list`/`watch`/`delete` on pods, `get` on `pods/log`, `create` on `pods/exec` and `pods/portforward`, and `create`/`delete` on services and persistentvolumeclaims. |
| `serviceAccountToken` | Explicit token for service-account auth. Leave empty when using kubeconfig or in-cluster config. |

!!! note
    The Kubernetes backend does not fall back to a simulation when the cluster is unreachable. A missing client or missing step image returns a real error so failures are visible in the execution log.

---

## Platform Settings

Agents appear in `configuration.yaml` under the `agents` list. Fields specific to remote agents:

| Field | Description |
|-------|-------------|
| `type` | `remote-agent` |
| `hostUrl` | Base URL of the worker process (e.g. `http://agent-host:8091`) |
| `tlsCert` / `tlsKey` | TLS credentials for encrypted connections |

!!! note
    Agents registered at runtime via `POST /api/v1/agents/register` are tracked in memory. Platform settings entries describe the *configured* backend targets — runtime registration is how a worker announces its current availability.

---

## See Also

- [Execution](execution.md) — how the control plane routes work to agents
- [Platform Settings](platform.md) — full agent configuration field reference
- [Operations](operations.md) — agent health and readiness checks

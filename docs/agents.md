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

1. **Register** — the agent calls `POST /api/v1/agents/register` with its identity and capabilities.
2. **Heartbeat** — the agent sends periodic heartbeats so the control plane knows it is alive.
3. **Claim** — the agent polls `POST /api/v1/agent-control/claims/next`. When a step is waiting, the control plane returns a `StepRequest`.
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

When `type: kubernetes` is set in platform settings, the control plane dispatches each step as an isolated Kubernetes pod in `targetNamespace`.

### How it works

1. A pod is created with the step image. Environment variables, headers, and OCI pull secrets are injected at pod creation time.
2. Container logs are streamed live with `follow: true` — output appears in real time rather than after the pod exits.
3. The control plane watches the **step container** status directly. This correctly detects completion even when artifact sidecars keep the pod running after the main container exits.
4. The container exit code is captured and evaluated against any `expect:` assertions defined in the step.
5. Artifact collection uses an `emptyDir` volume shared between the step container and a `busybox` sidecar. After the step container exits, artifacts are read through the pod exec API before the pod is deleted.
6. When the step completes the pod is deleted immediately. Cleanup is also triggered on cancellation.

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
| `targetNamespace` | Namespace where step pods are created. The service account must have `create`/`get`/`watch`/`delete` pod permissions in this namespace. |
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

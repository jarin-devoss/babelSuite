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

---
title: Environments
---

# Environments

[Back to index](index.md)

The Environments page is the runtime inventory for everything BabelSuite started — containers, networks, and volumes across all active and recent executions. Use it to see what is running and to clean up resources that were not automatically torn down.

---

## What It Shows

Each tracked environment corresponds to one execution run. The page displays:

| Field | Description |
|-------|-------------|
| Suite and profile | Which suite was run and under which profile |
| Owner | The user who launched the execution |
| Status | Current state — running, stopped, zombie |
| Started at | When the environment was created |
| Last heartbeat | When the orchestrator last reported in |
| Containers | All containers started for this run |
| Networks | Docker networks created for isolation |
| Volumes | Volumes mounted or created during the run |
| CPU / memory | Live resource usage (when available) |
| Warnings | Any detected anomalies |

---

## Zombie Detection

An environment is marked as a zombie when:

- The orchestrator process is no longer alive
- But containers, networks, or volumes from the run still exist

Zombie environments can be reaped individually or all at once.

---

## Live Updates

The page subscribes to a server-sent event stream. The stream replays the latest snapshot on connect, then pushes incremental changes as environment state changes.

```
GET /api/v1/sandboxes/events
```

---

## Cleanup

| Action | Description |
|--------|-------------|
| Reap one | Stops containers and removes networks and volumes for a single environment |
| Reap all | Cleans up every tracked environment in one operation |

The cleanup response reports how many containers, networks, and volumes were removed.

!!! note
    The backing API uses the path `/api/v1/sandboxes` — the UI calls it "Environments" but the server-side model retains the original name.

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/sandboxes` | List all tracked environments |
| `GET` | `/api/v1/sandboxes/events` | SSE stream of environment state changes |
| `POST` | `/api/v1/sandboxes/reap-all` | Clean up all tracked environments |
| `POST` | `/api/v1/sandboxes/{sandboxId}/reap` | Clean up a single environment |

---

## Frontend Route

| Route | Description |
|-------|-------------|
| `/environments` | Runtime inventory — containers, networks, volumes |

---

## See Also

- [Execution](execution.md) — how environments are created during a run
- [Operations](operations.md) — health probes and worker readiness

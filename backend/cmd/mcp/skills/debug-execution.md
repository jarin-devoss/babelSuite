---
name: debug-execution
description: Use when an execution failed, stalled, or produced unexpected results and you need to find out why
---

# Debug a Failed Execution

## Overview

A failed run reports a status but not a reason. The reason is in the per-step events and the container output, and the two say different things: events describe what the engine decided, logs show what actually ran.

## Prerequisites

- An authenticated session
- The execution ID, or `list_executions` to find it

## Workflow

1. **Find the run and its status:**
   ```
   list_executions()
   get_execution(execution_id="<id>")
   ```

2. **Read the step statuses, not just the top-level one.** The first `failed` step is the cause; everything `skipped` after it is a consequence, not a separate problem.

3. **Read what the containers actually printed.** Step events say a step failed; only the logs say why:
   ```
   get_execution_logs(execution_id="<id>")
   ```

4. **Separate environment faults from test failures.** If the first failure is an image pull, a missing network, or a service that exited immediately, the environment broke rather than the assertion:
   ```
   get_system_health()
   list_sandboxes()
   ```

5. **Check the profile if behaviour changed without the suite changing.** Profiles supply env vars and service overrides, so the same suite behaves differently under each:
   ```
   get_suite_profiles(suite_id="<suite>")
   ```

6. **Re-run against a different profile to isolate it:**
   ```
   launch_execution(suite_id="<suite>", profile="local.yaml")
   watch_execution(execution_id="<new id>")
   ```

## Reading Step Status

| Status | Meaning |
|--------|---------|
| `healthy` / `passed` | The step completed and its assertions held |
| `failed` | This step is the cause — start here |
| `skipped` | A dependency failed; not an independent failure |
| `pending` | Never started, because the run ended first |

## Common Errors

| Symptom | Cause | Fix |
|---------|-------|-----|
| `service exited immediately with code N` | The container crashed on boot — bad image, bad command, missing env | Read that step's logs; check the profile supplies what the service needs |
| `network ... not found` | The per-execution network could not be created, often because stale networks exhausted the address pool | Reap old sandboxes with `list_sandboxes` then `reap_sandbox` |
| Everything after one step is `skipped` | A hard dependency failed | Fix the first `failed` step; the rest will follow |
| A step passes locally but fails in a run | Depends on something the topology never declared | Add the dependency to `after=[...]`, or provision it in `pre-hook.star` |
| Run never leaves `Booting` | Topology resolution or the sidecar failed before steps were queued | `get_suite` and check `topologyError` |

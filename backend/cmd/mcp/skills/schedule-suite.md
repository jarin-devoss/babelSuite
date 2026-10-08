---
name: schedule-suite
description: Use when asked to run a suite on a schedule, nightly, periodically, or to set up recurring runs with notifications
---

# Schedule a Suite

## Overview

A cron job launches one or more suites on a cron expression and can notify by email or Slack when the run finishes. Each target names its own suite, profile, and backend, so one job can exercise several suites together.

## Prerequisites

- An authenticated session
- A suite that already launches successfully by hand — scheduling a broken suite just fails on a timer

## Workflow

1. **Confirm the suite and the exact profile and backend names:**
   ```
   list_suites()
   ```
   Use the `fileName` from the profile list (`local.yaml`), and a backend ID such as `local-docker`.

2. **Prove it runs before scheduling it:**
   ```
   launch_execution(suite_id="checkout-suite", profile="local.yaml", backend="local-docker")
   watch_execution(execution_id="<id>")
   ```

3. **Create the schedule:**
   ```
   create_cron_job(
     name="Nightly checkout",
     schedule="0 2 * * *",
     suites=[{"suiteId": "checkout-suite", "profile": "local.yaml", "backendId": "local-docker"}],
     email_recipients=["qa@example.test"],
     email_subject="Nightly checkout results"
   )
   ```

4. **Verify what was stored and when it next fires:**
   ```
   list_cron_jobs()
   get_cron_job(cron_job_id="<id>")
   ```

5. **Check it after its first tick.** `lastError` holds the failure reason if it did not launch:
   ```
   get_cron_job(cron_job_id="<id>")
   list_executions()
   ```

## Schedule Expressions

| Expression | Meaning |
|------------|---------|
| `0 2 * * *` | 02:00 every day |
| `*/15 * * * *` | Every fifteen minutes |
| `0 9 * * 1-5` | 09:00 on weekdays |
| `0 0 1 * *` | Midnight on the first of each month |

## Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| The job exists but never runs | `enabled` is false | `update_cron_job` with `enabled=true` |
| `lastError` names a missing profile | The profile was renamed or deleted | `get_suite_profiles`, then update the target |
| `lastError` names a missing backend | The agent is gone or offline | `list_agents` and pick a live backend |
| Fields vanish after an update | `update_cron_job` replaces the whole job | `get_cron_job` first and resend every field you want kept |
| Runs pile up | The schedule fires faster than the suite completes | Widen the interval, or narrow the suite |

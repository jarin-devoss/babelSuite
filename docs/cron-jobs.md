---
title: Cron Jobs
---

# Cron Jobs

[Back to index](index.md)

Cron jobs let you schedule one or more suites to run automatically on a recurring schedule. After each run, BabelSuite assembles a results report and delivers it via email or Slack — no manual steps required.

---

## How It Works

1. You define a cron job: a name, a cron schedule, one or more suite targets, and optional notification destinations.
2. At the scheduled time, BabelSuite launches each suite target as a separate execution on the configured backend.
3. Each execution is polled until it reaches a terminal state (up to 10 minutes per suite).
4. BabelSuite assembles a plain-text report: overall status, per-suite result, step counts, and duration.
5. The report is sent to every configured email recipient and Slack webhook.

Multiple suite targets in a single job run sequentially in the order they are defined.

---

## Cron Expressions

Schedules use standard five-field cron syntax:

```
┌─ minute   (0–59)
│ ┌─ hour    (0–23)
│ │ ┌─ day of month (1–31)
│ │ │ ┌─ month       (1–12)
│ │ │ │ ┌─ day of week (0–6, Sunday=0)
│ │ │ │ │
* * * * *
```

Common examples:

| Expression | When it runs |
|-----------|-------------|
| `0 8 * * *` | Every day at 08:00 |
| `0 */4 * * *` | Every 4 hours |
| `0 9 * * 1-5` | Weekdays at 09:00 |
| `30 22 * * 0` | Sundays at 22:30 |
| `*/15 * * * *` | Every 15 minutes |

---

## Suite Targets

Each cron job can run multiple suite targets. A target selects:

| Field | Description |
|-------|-------------|
| Suite | Which suite to execute |
| Profile | The launch profile to apply (env vars, secrets, module configuration) |
| Agent | Which execution backend to run on (local Docker, Kubernetes, a specific remote agent) |

Each target runs as a fully isolated execution. You can point different targets at different agents — for example, run a smoke suite on a local agent and a load suite on a Kubernetes cluster, all in a single scheduled job.

---

## Notifications

### Email

BabelSuite sends email using the SMTP server configured in **Settings → Notifications**. Each cron job can have:

- Any number of recipient addresses
- A custom subject line

If SMTP is not configured or the host is empty, email delivery is skipped and the error is recorded in the job's `lastError` field.

### Slack

Each cron job can have a Slack incoming webhook URL. When set, BabelSuite posts the same results report to the channel associated with that webhook.

To set one up:

1. Create an incoming webhook in your Slack workspace's app settings.
2. Copy the webhook URL (starts with `https://hooks.slack.com/`).
3. Paste it into the **Slack Webhook URL** field when creating or editing the cron job.

---

## Report Format

The results report BabelSuite generates looks like this:

```
BabelSuite — Cron Job Results: nightly-regression

Run time: 2026-05-23 02:00 UTC
Overall:  2/3 suites passed

─────────────────────────────────────────
✓  payment-suite           [ci]
   Steps: 14 healthy, 0 failed | Duration: 4m12s

✓  identity-broker         [staging]
   Steps: 9 healthy, 0 failed | Duration: 2m37s

✗  storefront-browser-lab  [local]
   Steps: 7 healthy, 3 failed | Duration: 6m04s
─────────────────────────────────────────
```

---

## Managing Cron Jobs

### UI

Navigate to **Cron Jobs** in the sidebar. From there you can:

- Create a new job (name, schedule, suite targets, notifications)
- Enable or disable an existing job without deleting it
- See the last run time, next scheduled run, and any error from the previous run

### API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/cron-jobs` | List all cron jobs |
| `POST` | `/api/v1/cron-jobs` | Create a cron job |
| `GET` | `/api/v1/cron-jobs/{id}` | Get a single cron job |
| `PUT` | `/api/v1/cron-jobs/{id}` | Update a cron job |
| `DELETE` | `/api/v1/cron-jobs/{id}` | Delete a cron job |

**Create / update payload:**

```json
{
  "name": "nightly-regression",
  "schedule": "0 2 * * *",
  "enabled": true,
  "suites": [
    {
      "suiteId": "payment-suite",
      "profile": "ci",
      "backendId": "local-docker"
    },
    {
      "suiteId": "identity-broker",
      "profile": "staging",
      "backendId": "k8s-runner"
    }
  ],
  "email": {
    "recipients": ["team@example.com", "oncall@example.com"],
    "subject": "Nightly Regression Results"
  },
  "slack": {
    "webhookUrl": "https://hooks.slack.com/services/T.../B.../..."
  }
}
```

---

## SMTP Configuration

SMTP settings live in `configuration.yaml` under `notifications.smtp` and are managed from **Settings → Notifications** in the UI.

```yaml
notifications:
  smtp:
    host: smtp.sendgrid.net
    port: 587
    username: apikey
    password: SG.xxxx
    from: BabelSuite <no-reply@example.com>
```

The password is stored on disk but is never returned by the API. When you save the Notifications settings page with the password field left blank, the existing password is preserved.

Changes to SMTP settings take effect on the next scheduled run — no server restart required.

---

## Disabling a Job

Setting `enabled: false` suspends scheduling without deleting the job or its history. The job can be re-enabled at any time and will resume on its next scheduled occurrence.

---

## Error Handling

If a suite execution fails to start or reaches an error state, BabelSuite records the error in the job's `lastError` field and includes it in the notification report. Execution failures in individual suite targets do not cancel the remaining targets — all configured targets run regardless.

If a notification channel fails (SMTP error, Slack webhook returning non-2xx), the error is logged but does not affect the job's recorded status.

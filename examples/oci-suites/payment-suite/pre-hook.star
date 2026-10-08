load("@babelsuite/runtime", "service", "task", "log")

# Readiness gate for the payments environment. Everything here runs to
# completion before the first suite.star step starts, and anything it leaves
# running stays up for the rest of the execution.
#
# The suite reconciles against a legacy ledger that is not part of suite.star,
# so the gate provisions it here and refuses to let the suite start until the
# ledger actually answers queries.

LEDGER_PASSWORD = "babelsuite"

# ── 1. provision the legacy ledger the reconciliation steps settle against ───
# Service containers keep running for the whole execution and get a network
# alias equal to their name, so suite steps reach this as "legacy-ledger-db".
ledger_db = service.run(
    name  = "legacy-ledger-db",
    image = "mysql:8",
    env   = {
        "MYSQL_ROOT_PASSWORD": LEDGER_PASSWORD,
        "MYSQL_DATABASE":      "ledger",
        "MYSQL_USER":          "ledger",
        "MYSQL_PASSWORD":      LEDGER_PASSWORD,
    },
)

# ── 2. starting the container only proves it did not crash on boot ──────────
# MySQL takes ten seconds or so to initialise its data directory, so poll it
# until it genuinely accepts connections rather than letting the suite race it.
ledger_alive = task.run(
    name  = "legacy-ledger-alive",
    image = "mysql:8",
    env   = {"MYSQL_PWD": LEDGER_PASSWORD},
    commands = [
        "attempt=0",
        "until mysqladmin ping --host=legacy-ledger-db --user=root --silent 2>/dev/null; do " +
        "  attempt=$((attempt+1)); " +
        "  if [ \"$attempt\" -ge 40 ]; then echo 'legacy ledger never accepted connections' >&2; exit 1; fi; " +
        "  echo \"waiting for legacy ledger (attempt $attempt)\"; " +
        "  sleep 2; " +
        "done",
        "echo 'legacy ledger is accepting connections'",
        # Answering ping is not the same as being able to serve the schema the
        # reconciliation steps will query.
        "mysql --host=legacy-ledger-db --user=root --database=ledger --execute='SELECT 1' >/dev/null",
        "echo 'legacy ledger schema is queryable'",
    ],
    after = [ledger_db],
)

# ── 3. the settlement API the suite calls out to, and proof it is alive ─────
settlement_api = service.run(
    name  = "settlement-api",
    image = "busybox",
    commands = [
        "mkdir -p /www",
        "echo '{\"status\":\"ok\",\"window\":\"open\"}' > /www/health",
        "httpd -f -p 8080 -h /www",
    ],
)

settlement_alive = task.run(
    name  = "settlement-api-alive",
    image = "busybox",
    commands = [
        "attempt=0",
        "until wget -q -O- http://settlement-api:8080/health 2>/dev/null | grep -q '\"status\":\"ok\"'; do " +
        "  attempt=$((attempt+1)); " +
        "  if [ \"$attempt\" -ge 30 ]; then echo 'settlement API never reported healthy' >&2; exit 1; fi; " +
        "  echo \"waiting for settlement API (attempt $attempt)\"; " +
        "  sleep 1; " +
        "done",
        "echo 'settlement API is alive and its window is open'",
    ],
    after = [settlement_api],
)

environment_ready = log.info(
    "payments environment ready — legacy ledger and settlement API are both live",
    after = [ledger_alive, settlement_alive],
)

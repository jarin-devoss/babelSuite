load("@babelsuite/runtime", "service", "task", "log")

# Readiness gate for the identity environment. Everything here runs to
# completion before the first suite.star step starts, and anything it leaves
# running stays up for the rest of the execution.
#
# The broker stores sessions in Redis and refuses to issue tokens if the
# upstream IdP's discovery document is unreachable, so the gate stands both up
# and proves they answer before the suite boots a single provider mock.

SESSION_BACKENDS = env.get("SESSION_BACKENDS", "jwt,cookie").split(",")

# ── 1. session store the broker's workers will share ────────────────────────
# Service containers keep running for the whole execution and get a network
# alias equal to their name, so suite steps reach this as "session-store".
session_store = service.run(
    name  = "session-store",
    image = "redis:7.2-alpine",
    commands = ["redis-server --save '' --appendonly no"],
)

# ── 2. a started container is not a ready one ───────────────────────────────
session_store_alive = task.run(
    name  = "session-store-alive",
    image = "redis:7.2-alpine",
    commands = [
        "attempt=0",
        "until [ \"$(redis-cli -h session-store ping 2>/dev/null)\" = 'PONG' ]; do " +
        "  attempt=$((attempt+1)); " +
        "  if [ \"$attempt\" -ge 30 ]; then echo 'session store never answered PING' >&2; exit 1; fi; " +
        "  echo \"waiting for session store (attempt $attempt)\"; " +
        "  sleep 1; " +
        "done",
        "echo 'session store answered PING'",
        # Answering PING is not the same as accepting the writes the session
        # workers will make, so round-trip a key before trusting it.
        "redis-cli -h session-store set babelsuite:gate ok >/dev/null",
        "test \"$(redis-cli -h session-store get babelsuite:gate)\" = 'ok' || (echo 'session store rejected a write' >&2; exit 1)",
        "redis-cli -h session-store del babelsuite:gate >/dev/null",
        "echo 'session store accepted a read-write round trip'",
    ],
    after = [session_store],
)

# ── 3. the upstream IdP the broker federates to ─────────────────────────────
upstream_idp = service.run(
    name  = "upstream-idp",
    image = "busybox",
    commands = [
        "mkdir -p /www/.well-known",
        "echo '{\"issuer\":\"http://upstream-idp:9000\",\"jwks_uri\":\"http://upstream-idp:9000/jwks\"}' > /www/.well-known/openid-configuration",
        "echo '{\"keys\":[]}' > /www/jwks",
        "httpd -f -p 9000 -h /www",
    ],
)

upstream_idp_alive = task.run(
    name  = "upstream-idp-alive",
    image = "busybox",
    commands = [
        "attempt=0",
        "until wget -q -O- http://upstream-idp:9000/.well-known/openid-configuration 2>/dev/null | grep -q 'issuer'; do " +
        "  attempt=$((attempt+1)); " +
        "  if [ \"$attempt\" -ge 30 ]; then echo 'upstream IdP discovery never became reachable' >&2; exit 1; fi; " +
        "  echo \"waiting for upstream IdP (attempt $attempt)\"; " +
        "  sleep 1; " +
        "done",
        "echo 'upstream IdP is serving its discovery document'",
        # The broker fetches the JWKS on first token validation; a discovery
        # document pointing at a dead JWKS endpoint fails much later and far
        # less obviously.
        "wget -q -O- http://upstream-idp:9000/jwks | grep -q 'keys' || (echo 'JWKS endpoint is not serving keys' >&2; exit 1)",
        "echo 'upstream IdP JWKS endpoint is live'",
    ],
    after = [upstream_idp],
)

# ── 4. only gate on the session store when the suite will actually use it ───
gates = [upstream_idp_alive]
if "redis" in SESSION_BACKENDS or "jwt" in SESSION_BACKENDS:
    gates.append(session_store_alive)

environment_ready = log.info(
    "identity environment ready — session store and upstream IdP are both live",
    after = gates,
)

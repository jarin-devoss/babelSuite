load("@babelsuite/redis",   "redis", "set_key", "set_keys", "flush_db", "wait_ready")
load("@babelsuite/runtime", "service", "task", "test")

FEATURE_FLAGS = {
    "checkout_v2":    "true",
    "fraud_shadow":   "false",
    "promo_engine":   "true",
}

SESSION_TTL = 3600   # 1 hour
CACHE_DBS   = [0, 1, 2]   # sessions, rate-limits, feature-flags

# start a password-protected cache with a 512 MB cap
cache = redis(
    name      = "cache",
    port      = 6380,
    password  = "s3cr3t",
    max_memory = "512mb",
    max_memory_policy = "volatile-lru",
)

ready = wait_ready(cache)

# flush all test databases before seeding
flush_nodes = []
for db in CACHE_DBS:
    f = flush_db(cache, db=db, after=[ready])
    flush_nodes.append(f)

# seed feature flags into db 2
seed_flags = set_keys(
    cache,
    mapping = FEATURE_FLAGS,
    ttl_seconds = None,
    after = flush_nodes,
)

# seed a few session tokens into db 0
SESSION_TOKENS = ["tok-aaa", "tok-bbb", "tok-ccc"]
session_nodes  = []
for token in SESSION_TOKENS:
    n = set_key(cache, key="session:" + token, value=token + "-payload", ttl_seconds=SESSION_TTL, after=[seed_flags])
    session_nodes.append(n)

# application that reads from the cache
app = service.run(
    name = "session-api",
    env  = {"REDIS_URL": cache["url"], "REDIS_PASSWORD": "s3cr3t"},
    after = session_nodes,
)

# smoke test that verifies TTL and feature flag reads
test.run(
    name  = "cache-smoke",
    file  = "cache_smoke.py",
    image = "python:3.12",
    after = [app],
    env   = {
        "REDIS_URL":      cache["url"],
        "FEATURE_FLAGS":  ",".join(FEATURE_FLAGS.keys()),
        "SESSION_TOKENS": ",".join(SESSION_TOKENS),
    },
)

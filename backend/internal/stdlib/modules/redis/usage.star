load("@babelsuite/redis",   "redis", "wait_ready", "flush_db", "set_keys", "set_key")
load("@babelsuite/runtime", "service", "test")

FEATURE_FLAGS = {
    "checkout_v2":  "true",
    "fraud_shadow": "false",
    "promo_engine": "true",
}

SESSION_TTL    = 3600
SESSION_TOKENS = ["tok-aaa", "tok-bbb", "tok-ccc"]

cache = redis(name="cache", max_memory="512mb", max_memory_policy="volatile-lru")

ready = wait_ready(cache)
flush = flush_db(cache, db=0, after=[ready])

seed_flags = set_keys(cache, mapping=FEATURE_FLAGS, after=[flush])

session_nodes = []
for token in SESSION_TOKENS:
    n = set_key(cache, key="session:" + token, value=token + "-payload", ttl_seconds=SESSION_TTL, after=[seed_flags])
    session_nodes.append(n)

app = service.run(
    name  = "session-api",
    env   = {"REDIS_HOST": cache.name},
    after = session_nodes,
)

test.run(
    name  = "cache-smoke",
    file  = "cache_smoke.py",
    image = "python:3.12",
    after = [app],
    env   = {
        "REDIS_HOST":     cache.name,
        "FEATURE_FLAGS":  ",".join(FEATURE_FLAGS.keys()),
        "SESSION_TOKENS": ",".join(SESSION_TOKENS),
    },
)

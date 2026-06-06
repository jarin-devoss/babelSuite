load("@babelsuite/runtime", "task")
load("_shared.star", "sanitize_name")

def _cli(cache, name, cmd, image = "redis:7.2-alpine", after = []):
    host = '"${REDIS_HOST:-' + cache.name + '}"'
    port = '"${REDIS_PORT:-6379}"'
    auth = '${REDIS_PASSWORD:+-a "$REDIS_PASSWORD"}'
    prefix = "redis-cli -h " + host + " -p " + port + " " + auth
    return task.run(
        name     = name,
        image    = image,
        after    = [cache] + after,
        commands = [prefix + " " + cmd],
    )

def wait_ready(cache, image = "redis:7.2-alpine", after = []):
    host = '"${REDIS_HOST:-' + cache.name + '}"'
    port = '"${REDIS_PORT:-6379}"'
    auth = '${REDIS_PASSWORD:+-a "$REDIS_PASSWORD"}'
    probe = "redis-cli -h " + host + " -p " + port + " " + auth + " PING"
    return task.run(
        name     = cache.name + "-wait-ready",
        image    = image,
        after    = [cache] + after,
        commands = ["until " + probe + " | grep -q PONG; do sleep 1; done"],
    )

def set_key(cache, key, value, ttl_seconds = None, image = "redis:7.2-alpine", after = []):
    cmd = "SET '" + key + "' '" + str(value) + "'"
    if ttl_seconds != None:
        cmd += " EX " + str(ttl_seconds)
    return _cli(cache, cache.name + "-set-" + sanitize_name(key), cmd, image=image, after=after)

def set_keys(cache, mapping, ttl_seconds = None, image = "redis:7.2-alpine", after = []):
    parts = []
    for key, value in mapping.items():
        cmd = "SET '" + key + "' '" + str(value) + "'"
        if ttl_seconds != None:
            cmd += " EX " + str(ttl_seconds)
        parts.append(cmd)
    return task.run(
        name     = cache.name + "-set-keys",
        image    = image,
        after    = [cache] + after,
        commands = [" && ".join([
            "redis-cli -h \"${REDIS_HOST:-" + cache.name + "}\" -p \"${REDIS_PORT:-6379}\" ${REDIS_PASSWORD:+-a \"$REDIS_PASSWORD\"} " + p
            for p in parts
        ])],
    )

def delete_key(cache, key, image = "redis:7.2-alpine", after = []):
    return _cli(cache, cache.name + "-del-" + sanitize_name(key), "DEL '" + key + "'", image=image, after=after)

def flush_db(cache, db = 0, image = "redis:7.2-alpine", after = []):
    return _cli(cache, cache.name + "-flushdb-" + str(db), "-n " + str(db) + " FLUSHDB", image=image, after=after)

def flush_all(cache, image = "redis:7.2-alpine", after = []):
    return _cli(cache, cache.name + "-flushall", "FLUSHALL", image=image, after=after)

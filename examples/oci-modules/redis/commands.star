load("@babelsuite/runtime", "task")

# _redis_cli is the redis-cli command for a cache, built from what the service
# was created with (cache.env): its port and password, so redis(port=, password=)
# is honoured by every helper. REDIS_HOST and REDIS_PORT in the step's env still
# override the address. The password goes in REDISCLI_AUTH, which redis-cli reads
# without the warning that -a prints.
def _redis_cli(cache):
    env = cache.env
    auth = ""
    if env.get("REDIS_PASSWORD"):
        auth = "REDISCLI_AUTH=" + utils.quoted(env["REDIS_PASSWORD"]) + " "
    return (
        auth + 'redis-cli -h "${REDIS_HOST:-' + cache.name + '}"'
        + ' -p "${REDIS_PORT:-' + env.get("REDIS_PORT", "6379") + '}"'
    )

def _cli(cache, name, cmd, image = "redis:7.2-alpine", after = []):
    prefix = _redis_cli(cache)
    return task.run(
        name     = name,
        image    = image,
        after    = [cache] + after,
        commands = [prefix + " " + cmd],
    )

def wait_ready(cache, image = "redis:7.2-alpine", after = []):
    probe = _redis_cli(cache) + " PING"
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
    return _cli(cache, cache.name + "-set-" + utils.sanitize(key), cmd, image=image, after=after)

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
            _redis_cli(cache) + " " + p
            for p in parts
        ])],
    )

def delete_key(cache, key, image = "redis:7.2-alpine", after = []):
    return _cli(cache, cache.name + "-del-" + utils.sanitize(key), "DEL '" + key + "'", image=image, after=after)

def flush_db(cache, db = 0, image = "redis:7.2-alpine", after = []):
    return _cli(cache, cache.name + "-flushdb-" + str(db), "-n " + str(db) + " FLUSHDB", image=image, after=after)

def flush_all(cache, image = "redis:7.2-alpine", after = []):
    return _cli(cache, cache.name + "-flushall", "FLUSHALL", image=image, after=after)

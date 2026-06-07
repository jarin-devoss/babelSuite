load("@babelsuite/runtime", "service")

def redis(
        name              = "redis",
        image             = "redis:7.2-alpine",
        port              = 6379,
        password          = None,
        max_memory        = "256mb",
        max_memory_policy = "allkeys-lru",
        persistence       = False,
        databases         = 16,
        after             = [],
        env               = {}):
    base_env = {
        "REDIS_PORT":             str(port),
        "REDIS_DATABASES":        str(databases),
        "REDIS_MAXMEMORY":        max_memory,
        "REDIS_MAXMEMORY_POLICY": max_memory_policy,
        "REDIS_SAVE":             "" if not persistence else "3600 1",
    }
    if password != None:
        base_env["REDIS_PASSWORD"] = password
    if persistence:
        base_env["REDIS_APPENDONLY"] = "yes"
        base_env["REDIS_APPENDFSYNC"] = "everysec"

    return service.run(
        name  = name,
        image = image,
        after = after,
        env   = utils.merge(base_env, env),
    )

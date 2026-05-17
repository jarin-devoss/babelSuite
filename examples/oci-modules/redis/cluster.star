load("@babelsuite/runtime", "service")
load("_shared.star", "merge_dicts")

def redis(
        name = "redis",
        image = "redis:7.2-alpine",
        port = 6379,
        password = None,
        max_memory = "256mb",
        max_memory_policy = "allkeys-lru",
        persistence = False,
        databases = 16,
        after = [],
        env = {}):

    base_env = {
        "REDIS_PORT": str(port),
        "REDIS_DATABASES": str(databases),
        "REDIS_MAXMEMORY": max_memory,
        "REDIS_MAXMEMORY_POLICY": max_memory_policy,
    }

    if password != None:
        base_env["REDIS_PASSWORD"] = password

    if persistence:
        base_env["REDIS_APPENDONLY"] = "yes"
        base_env["REDIS_APPENDFSYNC"] = "everysec"
    else:
        base_env["REDIS_SAVE"] = ""

    node = service.run(
        name = name,
        image = image,
        after = after,
        env = merge_dicts(base_env, env),
        ports = {"6379": port},
    )

    return {
        "service":   node,
        "name":      name,
        "host":      name,
        "port":      port,
        "password":  password,
        "databases": databases,
        "image":     image,
        "url":       "redis://" + name + ":" + str(port),
    }

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
    # redis-server reads none of these as environment variables, so they are
    # given to it as arguments. The same values stay in the node's env, which is
    # how the helpers (commands.star) learn the port and the password.
    base_env = {"REDIS_PORT": str(port)}
    if password != None:
        base_env["REDIS_PASSWORD"] = password

    args = [
        "--port", str(port),
        "--databases", str(databases),
        "--maxmemory", max_memory,
        "--maxmemory-policy", max_memory_policy,
    ]
    if password != None:
        args += ["--requirepass", password]
    if persistence:
        args += ["--save", "3600 1", "--appendonly", "yes", "--appendfsync", "everysec"]
    else:
        args += ["--save", "", "--appendonly", "no"]

    return service.run(
        name     = name,
        image    = image,
        after    = after,
        env      = utils.merge(base_env, env),
        commands = ["exec redis-server " + " ".join([utils.quoted(a) for a in args])],
    )

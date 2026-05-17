load("@babelsuite/runtime", "service")
load("_shared.star", "merge_dicts")

def mongodb(
        name = "mongodb",
        image = "mongo:7.0",
        port = 27017,
        username = None,
        password = None,
        replica_set = None,
        wired_tiger_cache_gb = None,
        after = [],
        env = {}):

    base_env = {"MONGO_PORT": str(port)}

    if username != None and password != None:
        base_env["MONGO_INITDB_ROOT_USERNAME"] = username
        base_env["MONGO_INITDB_ROOT_PASSWORD"] = password

    if replica_set != None:
        base_env["MONGO_REPLICA_SET_NAME"] = replica_set

    if wired_tiger_cache_gb != None:
        base_env["MONGO_WIRED_TIGER_CACHE_SIZE_GB"] = str(wired_tiger_cache_gb)

    node = service.run(
        name  = name,
        image = image,
        after = after,
        env   = merge_dicts(base_env, env),
        ports = {"27017": port},
    )

    uri = "mongodb://"
    if username != None and password != None:
        uri += username + ":" + password + "@"
    uri += name + ":" + str(port)

    return {
        "service":     node,
        "name":        name,
        "host":        name,
        "port":        port,
        "username":    username,
        "password":    password,
        "replica_set": replica_set,
        "image":       image,
        "uri":         uri,
    }

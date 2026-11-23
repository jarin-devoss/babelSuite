load("@babelsuite/runtime", "service", "task")

# mongodb starts a MongoDB service. mongod reads none of its options from the
# environment, so port, replica set and cache size are given to it as arguments.
# The values stay in the node's env, which is how the helpers (collections.star)
# learn the port and the credentials.
#
# With replica_set, a task (<name>-rs-init) initiates the set once mongod is up;
# the helpers wait for a primary before they run. A replica set with credentials
# needs a keyfile for its members to authenticate each other, which the command
# writes before the image's entrypoint starts mongod.
def mongodb(
        name                 = "mongodb",
        image                = "mongo:7.0",
        port                 = 27017,
        username             = None,
        password             = None,
        replica_set          = None,
        wired_tiger_cache_gb = None,
        after                = [],
        env                  = {}):
    if (username == None) != (password == None):
        fail("mongodb needs username= and password= together")

    base_env = {"MONGO_PORT": str(port)}
    if username != None:
        base_env["MONGO_INITDB_ROOT_USERNAME"] = username
        base_env["MONGO_INITDB_ROOT_PASSWORD"] = password
    if replica_set != None:
        base_env["MONGO_REPLICA_SET_NAME"] = replica_set
    if wired_tiger_cache_gb != None:
        base_env["MONGO_WIRED_TIGER_CACHE_SIZE_GB"] = str(wired_tiger_cache_gb)

    args = ["--bind_ip_all", "--port", str(port)]
    if replica_set != None:
        args += ["--replSet", replica_set]
    if wired_tiger_cache_gb != None:
        args += ["--wiredTigerCacheSizeGB", str(wired_tiger_cache_gb)]

    command = ""
    if replica_set != None and username != None:
        command += (
            "openssl rand -base64 756 > /tmp/mongo-keyfile && chmod 400 /tmp/mongo-keyfile "
            + "&& chown mongodb:mongodb /tmp/mongo-keyfile && "
        )
        args += ["--keyFile", "/tmp/mongo-keyfile"]
    command += "exec docker-entrypoint.sh mongod " + " ".join([utils.quoted(a) for a in args])

    node = service.run(
        name     = name,
        image    = image,
        after    = after,
        env      = utils.merge(base_env, env),
        commands = [command],
    )

    if replica_set != None:
        member = name + ":" + str(port)
        initiate = (
            "try { rs.status() } catch (e) { rs.initiate({_id: " + utils.js_value(replica_set)
            + ", members: [{_id: 0, host: " + utils.js_value(member) + "}]}) }"
        )
        task.run(
            name     = name + "-rs-init",
            image    = image,
            after    = [node],
            commands = [
                "until " + _mongosh_base(name, port, username, password) + " --quiet --eval 'db.runCommand({ping: 1})' >/dev/null 2>&1; do sleep 1; done",
                _mongosh_base(name, port, username, password) + " --quiet --eval " + utils.quoted(initiate),
                "until " + _mongosh_base(name, port, username, password) + " --quiet --eval 'quit(db.hello().isWritablePrimary ? 0 : 1)' >/dev/null 2>&1; do sleep 1; done",
            ],
        )
    return node

def _mongosh_base(host, port, username, password):
    cmd = "mongosh --host " + utils.quoted(host) + " --port " + str(port)
    if username != None:
        cmd += " --username " + utils.quoted(username) + " --password " + utils.quoted(password) + " --authenticationDatabase admin"
    return cmd

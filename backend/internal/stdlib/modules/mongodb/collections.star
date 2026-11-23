load("@babelsuite/runtime", "task")

# _connection is the mongosh command line for a cluster, built from what the
# service was created with (cluster.env): its port and, when it has them, its
# credentials, so mongodb(port=, username=, password=) is honoured by every
# helper. The credentials go in flags, which need no URL encoding. A MONGO_URI in
# the step's env (a suite dependency's input, say) still wins, as the address.
def _connection(cluster, db):
    env = cluster.env
    uri = '"${MONGO_URI:-mongodb://' + cluster.name + ":" + env.get("MONGO_PORT", "27017") + '}/' + db + '"'
    cmd = "mongosh " + uri
    if env.get("MONGO_INITDB_ROOT_USERNAME"):
        cmd += (
            " --username " + utils.quoted(env["MONGO_INITDB_ROOT_USERNAME"])
            + " --password " + utils.quoted(env.get("MONGO_INITDB_ROOT_PASSWORD", ""))
            + " --authenticationDatabase admin"
        )
    return cmd

# _until_primary waits for a writable primary, which a replica set takes a moment
# to elect after its members start. A standalone server is one at once.
def _until_primary(connection):
    return "for i in $(seq 1 60); do " + connection + " --quiet --eval 'quit(db.hello().isWritablePrimary ? 0 : 1)' >/dev/null 2>&1 && break; sleep 1; done"

def _mongosh(cluster, name, db, js, image = "mongo:7.0", after = []):
    connection = _connection(cluster, db)
    return task.run(
        name     = name,
        image    = image,
        after    = [cluster] + after,
        commands = [_until_primary(connection), connection + " --eval '" + js.replace("'", "'\\''") + "'"],
    )

def create_collection(cluster, db, collection, validator = None, image = "mongo:7.0", after = []):
    if validator != None:
        js = "db.createCollection('" + collection + "', { validator: " + validator + " })"
    else:
        js = "db.createCollection('" + collection + "')"
    return _mongosh(
        cluster,
        name  = cluster.name + "-create-" + utils.sanitize(db) + "-" + utils.sanitize(collection),
        db    = db,
        js    = js,
        image = image,
        after = after,
    )

def create_index(cluster, db, collection, keys, unique = False, sparse = False, image = "mongo:7.0", after = []):
    key_parts = []
    for field, direction in keys.items():
        key_parts.append('"' + field + '": ' + str(direction))
    opts = []
    if unique:
        opts.append('"unique": true')
    if sparse:
        opts.append('"sparse": true')
    opts_js = "{" + ", ".join(opts) + "}" if opts else "{}"
    js = "db." + collection + ".createIndex({" + ", ".join(key_parts) + "}, " + opts_js + ")"
    return _mongosh(
        cluster,
        name  = cluster.name + "-idx-" + utils.sanitize(collection) + "-" + utils.sanitize(",".join(keys.keys())),
        db    = db,
        js    = js,
        image = image,
        after = after,
    )

def insert_documents(cluster, db, collection, documents, image = "mongo:7.0", after = []):
    doc_parts = []
    for doc in documents:
        field_parts = []
        for key, value in doc.items():
            field_parts.append('"' + key + '": ' + utils.js_value(value))
        doc_parts.append("{" + ", ".join(field_parts) + "}")
    js = "db." + collection + ".insertMany([" + ", ".join(doc_parts) + "])"
    return _mongosh(
        cluster,
        name  = cluster.name + "-insert-" + utils.sanitize(db) + "-" + utils.sanitize(collection),
        db    = db,
        js    = js,
        image = image,
        after = after,
    )

def drop_collection(cluster, db, collection, image = "mongo:7.0", after = []):
    return _mongosh(
        cluster,
        name  = cluster.name + "-drop-" + utils.sanitize(db) + "-" + utils.sanitize(collection),
        db    = db,
        js    = "db." + collection + ".drop()",
        image = image,
        after = after,
    )

def run_script(cluster, db, script_path, image = "mongo:7.0", after = []):
    connection = _connection(cluster, db)
    return task.run(
        name     = cluster.name + "-script-" + utils.sanitize(db) + "-" + utils.sanitize(script_path),
        image    = image,
        after    = [cluster] + after,
        commands = [_until_primary(connection), connection + " " + script_path],
    )

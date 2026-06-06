load("@babelsuite/runtime", "task")
load("_shared.star", "sanitize_name", "js_value")

def _mongosh(cluster, name, db, js, image = "mongo:7.0", after = []):
    uri = '"${MONGO_URI:-mongodb://' + cluster.name + ':27017}"'
    return task.run(
        name     = name,
        image    = image,
        after    = [cluster] + after,
        commands = ["mongosh " + uri + "/" + db + ' --eval \'' + js.replace("'", "'\\''") + "'"],
    )

def create_collection(cluster, db, collection, validator = None, image = "mongo:7.0", after = []):
    if validator != None:
        js = "db.createCollection('" + collection + "', { validator: " + validator + " })"
    else:
        js = "db.createCollection('" + collection + "')"
    return _mongosh(
        cluster,
        name  = cluster.name + "-create-" + sanitize_name(db) + "-" + sanitize_name(collection),
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
        name  = cluster.name + "-idx-" + sanitize_name(collection) + "-" + sanitize_name(",".join(keys.keys())),
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
            field_parts.append('"' + key + '": ' + js_value(value))
        doc_parts.append("{" + ", ".join(field_parts) + "}")
    js = "db." + collection + ".insertMany([" + ", ".join(doc_parts) + "])"
    return _mongosh(
        cluster,
        name  = cluster.name + "-insert-" + sanitize_name(db) + "-" + sanitize_name(collection),
        db    = db,
        js    = js,
        image = image,
        after = after,
    )

def drop_collection(cluster, db, collection, image = "mongo:7.0", after = []):
    return _mongosh(
        cluster,
        name  = cluster.name + "-drop-" + sanitize_name(db) + "-" + sanitize_name(collection),
        db    = db,
        js    = "db." + collection + ".drop()",
        image = image,
        after = after,
    )

def run_script(cluster, db, script_path, image = "mongo:7.0", after = []):
    uri = '"${MONGO_URI:-mongodb://' + cluster.name + ':27017}"'
    return task.run(
        name     = cluster.name + "-script-" + sanitize_name(db) + "-" + sanitize_name(script_path),
        image    = image,
        after    = [cluster] + after,
        commands = ["mongosh " + uri + "/" + db + " " + script_path],
    )

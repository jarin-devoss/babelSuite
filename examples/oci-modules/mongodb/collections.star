load("@babelsuite/runtime", "task")
load("_shared.star", "merge_dicts", "merge_after", "sanitize_name", "mongosh_prefix", "js_value")

def _mongosh_task(cluster, name, db, js, after = [], env = {}):
    script = mongosh_prefix(cluster) + "/" + db + ' --eval "' + js.replace('"', '\\"') + '"'
    return task.run(
        name    = name,
        image   = cluster["image"],
        after   = merge_after(cluster, after),
        env     = merge_dicts({"MONGO_URI": cluster["uri"]}, env),
        command = ["bash", "-lc", script],
    )

def create_collection(cluster, db, collection, validator = None, after = []):
    if validator != None:
        js = "db.createCollection('" + collection + "', { validator: " + validator + " })"
    else:
        js = "db.createCollection('" + collection + "')"
    return _mongosh_task(
        cluster,
        name = cluster["name"] + "-create-" + sanitize_name(db) + "-" + sanitize_name(collection),
        db   = db,
        js   = js,
        after = after,
    )

def create_index(cluster, db, collection, keys, unique = False, sparse = False, after = []):
    key_parts = []
    for field, direction in keys.items():
        key_parts.append('"' + field + '": ' + str(direction))
    keys_js = "{" + ", ".join(key_parts) + "}"
    opts = []
    if unique:
        opts.append('"unique": true')
    if sparse:
        opts.append('"sparse": true')
    opts_js = "{" + ", ".join(opts) + "}" if len(opts) > 0 else "{}"
    js = "db." + collection + ".createIndex(" + keys_js + ", " + opts_js + ")"
    return _mongosh_task(
        cluster,
        name  = cluster["name"] + "-idx-" + sanitize_name(collection) + "-" + sanitize_name(",".join(keys.keys())),
        db    = db,
        js    = js,
        after = after,
    )

def insert_documents(cluster, db, collection, documents, after = []):
    doc_parts = []
    for doc in documents:
        field_parts = []
        for key, value in doc.items():
            field_parts.append('"' + key + '": ' + js_value(value))
        doc_parts.append("{" + ", ".join(field_parts) + "}")
    js = "db." + collection + ".insertMany([" + ", ".join(doc_parts) + "])"
    return _mongosh_task(
        cluster,
        name  = cluster["name"] + "-insert-" + sanitize_name(db) + "-" + sanitize_name(collection),
        db    = db,
        js    = js,
        after = after,
    )

def drop_collection(cluster, db, collection, after = []):
    return _mongosh_task(
        cluster,
        name  = cluster["name"] + "-drop-" + sanitize_name(db) + "-" + sanitize_name(collection),
        db    = db,
        js    = "db." + collection + ".drop()",
        after = after,
    )

def run_script(cluster, db, script_path, after = [], env = {}):
    cmd = mongosh_prefix(cluster) + "/" + db + " " + script_path
    return task.run(
        name    = cluster["name"] + "-script-" + sanitize_name(db) + "-" + sanitize_name(script_path),
        image   = cluster["image"],
        after   = merge_after(cluster, after),
        env     = merge_dicts({"MONGO_URI": cluster["uri"]}, env),
        command = ["bash", "-lc", cmd],
    )

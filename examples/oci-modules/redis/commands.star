load("@babelsuite/runtime", "task")
load("_shared.star", "merge_dicts", "merge_after", "quoted", "sanitize_name", "redis_cli_prefix")

def _cmd_task(cluster, name, script_body, after = [], env = {}):
    return task.run(
        name = name,
        image = cluster["image"],
        after = merge_after(cluster, after),
        env = merge_dicts({"REDIS_URL": cluster["url"]}, env),
        command = ["sh", "-c", script_body],
    )

def set_key(cluster, key, value, ttl_seconds = None, after = []):
    cmd = redis_cli_prefix(cluster) + " SET " + quoted(key) + " " + quoted(str(value))
    if ttl_seconds != None:
        cmd += " EX " + str(ttl_seconds)
    return _cmd_task(
        cluster,
        name = cluster["name"] + "-set-" + sanitize_name(key),
        script_body = cmd,
        after = after,
    )

def delete_key(cluster, key, after = []):
    return _cmd_task(
        cluster,
        name = cluster["name"] + "-del-" + sanitize_name(key),
        script_body = redis_cli_prefix(cluster) + " DEL " + quoted(key),
        after = after,
    )

def flush_db(cluster, db = 0, after = []):
    cmd = redis_cli_prefix(cluster) + " -n " + str(db) + " FLUSHDB"
    return _cmd_task(
        cluster,
        name = cluster["name"] + "-flushdb-" + str(db),
        script_body = cmd,
        after = after,
    )

def flush_all(cluster, after = []):
    return _cmd_task(
        cluster,
        name = cluster["name"] + "-flushall",
        script_body = redis_cli_prefix(cluster) + " FLUSHALL",
        after = after,
    )

def set_keys(cluster, mapping, ttl_seconds = None, after = []):
    lines = []
    for key, value in mapping.items():
        cmd = redis_cli_prefix(cluster) + " SET " + quoted(key) + " " + quoted(str(value))
        if ttl_seconds != None:
            cmd += " EX " + str(ttl_seconds)
        lines.append(cmd)
    return _cmd_task(
        cluster,
        name = cluster["name"] + "-set-keys-batch",
        script_body = " && ".join(lines),
        after = after,
    )

def wait_ready(cluster, after = []):
    cmd = (
        "until " + redis_cli_prefix(cluster) + " PING | grep -q PONG; do"
        + " echo 'waiting for redis...'; sleep 1; done"
    )
    return _cmd_task(
        cluster,
        name = cluster["name"] + "-wait-ready",
        script_body = cmd,
        after = after,
    )

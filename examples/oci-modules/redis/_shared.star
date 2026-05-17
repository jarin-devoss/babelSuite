def merge_dicts(base, overrides):
    merged = {}
    for key, value in base.items():
        merged[key] = value
    for key, value in overrides.items():
        merged[key] = value
    return merged

def merge_after(cluster, after):
    merged = []
    seen = {}
    for item in after:
        if item not in seen:
            merged.append(item)
            seen[item] = True
    if cluster["name"] not in seen:
        merged.append(cluster["name"])
    return merged

def sanitize_name(value):
    output = ""
    for ch in str(value):
        if ("a" <= ch and ch <= "z") or ("A" <= ch and ch <= "Z") or ("0" <= ch and ch <= "9"):
            output += ch.lower()
        else:
            output += "-"
    while "--" in output:
        output = output.replace("--", "-")
    if output.startswith("-"):
        output = output[1:]
    if output.endswith("-"):
        output = output[:-1]
    return output or "step"

def quoted(value):
    return "'" + str(value).replace("'", "'\"'\"'") + "'"

def redis_cli_prefix(cluster):
    cmd = "redis-cli -h " + quoted(cluster["host"]) + " -p " + str(cluster["port"])
    if cluster["password"] != None:
        cmd += " -a " + quoted(cluster["password"])
    return cmd

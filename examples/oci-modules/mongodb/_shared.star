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

def mongosh_prefix(cluster):
    uri = "mongodb://"
    if cluster["username"] != None and cluster["password"] != None:
        uri += cluster["username"] + ":" + cluster["password"] + "@"
    uri += cluster["host"] + ":" + str(cluster["port"])
    return "mongosh '" + uri + "'"

def js_value(v):
    t = type(v)
    if t == "string":
        return '"' + v + '"'
    elif t == "bool":
        return "true" if v else "false"
    elif t == "NoneType":
        return "null"
    else:
        return str(v)

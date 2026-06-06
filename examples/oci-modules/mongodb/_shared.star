def sanitize_name(value):
    s = str(value)
    output = ""
    for i in range(len(s)):
        ch = s[i]
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

def merge_dicts(base, overrides):
    merged = {}
    for key, value in base.items():
        merged[key] = value
    for key, value in overrides.items():
        merged[key] = value
    return merged

def js_value(v):
    t = type(v)
    if t == "string":
        return '"' + v.replace('"', '\\"') + '"'
    if t == "bool":
        return "true" if v else "false"
    if t == "NoneType":
        return "null"
    return str(v)

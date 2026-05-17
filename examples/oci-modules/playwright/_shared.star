SUPPORTED_BROWSERS = ["chromium", "firefox", "webkit"]

DEVICE_PRESETS = {
    "desktop":      {"width": 1280, "height": 800,  "device_scale": 1, "is_mobile": False},
    "mobile":       {"width": 390,  "height": 844,  "device_scale": 3, "is_mobile": True},
    "tablet":       {"width": 768,  "height": 1024, "device_scale": 2, "is_mobile": True},
    "desktop-hd":   {"width": 1920, "height": 1080, "device_scale": 1, "is_mobile": False},
}

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

def merge_dicts(base, overrides):
    merged = {}
    for key, value in base.items():
        merged[key] = value
    for key, value in overrides.items():
        merged[key] = value
    return merged

def default_exports(name):
    return [
        {"path": "reports/" + name + "-junit.xml",  "name": name,           "on": "always", "format": "junit"},
        {"path": "traces/"  + name + ".zip",         "name": name + "-trace", "on": "failure"},
        {"path": "screenshots/" + name,              "name": name + "-shots", "on": "failure"},
    ]

SUPPORTED_BROWSERS = ["chromium", "firefox", "webkit"]

DEVICE_PRESETS = {
    "desktop":    {"width": 1280, "height": 800,  "device_scale": 1, "is_mobile": False},
    "mobile":     {"width": 390,  "height": 844,  "device_scale": 3, "is_mobile": True},
    "tablet":     {"width": 768,  "height": 1024, "device_scale": 2, "is_mobile": True},
    "desktop-hd": {"width": 1920, "height": 1080, "device_scale": 1, "is_mobile": False},
}

def default_exports(name):
    return [
        {"path": "reports/" + name + "-junit.xml",  "name": name,           "on": "always", "format": "junit"},
        {"path": "traces/"  + name + ".zip",         "name": name + "-trace", "on": "failure"},
        {"path": "screenshots/" + name,              "name": name + "-shots", "on": "failure"},
    ]

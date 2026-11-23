# The Playwright version the module runs specs with. The image and the
# @playwright/test package must be the same version (the package finds its
# browsers in the image by version), so both come from this one constant.
PLAYWRIGHT_VERSION = "1.53.0"
PLAYWRIGHT_IMAGE   = "mcr.microsoft.com/playwright:v" + PLAYWRIGHT_VERSION + "-noble"

# playwright_commands runs a spec with the Playwright runner. The spec is shipped
# with file= and not run by it: file= would run a .ts file with node, which is not
# the test runner and fails before the test body. @playwright/test is installed
# next to the spec at the image's version, and the JUnit reporter writes to the
# path the step exports.
def playwright_commands(spec, browser, junit_path, version = PLAYWRIGHT_VERSION, extra_args = ""):
    command = "npx playwright test " + utils.quoted(spec) + " --browser=" + browser + " --reporter=junit,line"
    if extra_args != "":
        command += " " + extra_args
    return [
        "npm install --no-save --no-audit --no-fund @playwright/test@" + version,
        "PLAYWRIGHT_JUNIT_OUTPUT_NAME=" + utils.quoted(junit_path) + " " + command,
    ]

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

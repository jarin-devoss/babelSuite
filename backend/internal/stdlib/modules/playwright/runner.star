load("@babelsuite/runtime", "test")
load("_shared.star", "SUPPORTED_BROWSERS", "DEVICE_PRESETS", "sanitize_name", "merge_dicts", "default_exports")

def browser_test(
        spec,
        base_url,
        browsers = ["chromium"],
        devices = ["desktop"],
        after = [],
        env = {},
        collect_traces = True,
        collect_video = False,
        extra_exports = [],
        image = "mcr.microsoft.com/playwright:v1.53.0-noble"):
    nodes = []

    for browser in browsers:
        if browser not in SUPPORTED_BROWSERS:
            fail("unsupported browser: " + browser + ". Must be one of: " + ", ".join(SUPPORTED_BROWSERS))

        for device in devices:
            if device not in DEVICE_PRESETS:
                fail("unsupported device preset: " + device + ". Must be one of: " + ", ".join(DEVICE_PRESETS.keys()))

            preset    = DEVICE_PRESETS[device]
            test_name = sanitize_name(spec) + "-" + browser + "-" + device

            base_env = {
                "BROWSER":           browser,
                "BASE_URL":          base_url,
                "DEVICE":            device,
                "VIEWPORT_WIDTH":    str(preset["width"]),
                "VIEWPORT_HEIGHT":   str(preset["height"]),
                "DEVICE_SCALE":      str(preset["device_scale"]),
                "IS_MOBILE":         str(preset["is_mobile"]).lower(),
                "COLLECT_TRACES":    str(collect_traces).lower(),
                "COLLECT_VIDEO":     str(collect_video).lower(),
            }

            exports = default_exports(test_name) + extra_exports
            if collect_video:
                exports.append({"path": "videos/" + test_name, "name": test_name + "-video", "on": "failure"})

            node = test.run(
                name    = test_name,
                file    = spec,
                image   = image,
                after   = after,
                env     = merge_dicts(base_env, env),
                exports = exports,
            )
            nodes.append(node)

    return nodes

def a11y_audit(
        url,
        after = [],
        wcag_level = "AA",
        fail_on_critical = True,
        image = "mcr.microsoft.com/playwright:v1.53.0-noble"):
    audit_name = "a11y-audit-" + sanitize_name(url)
    fail_logs  = ["AXE_CRITICAL"] if fail_on_critical else []

    return test.run(
        name    = audit_name,
        file    = "a11y_audit.spec.ts",
        image   = image,
        after   = after,
        env     = {"TARGET_URL": url, "WCAG_LEVEL": wcag_level},
        fail_on_logs = fail_logs,
        exports = [
            {"path": "reports/" + audit_name + ".xml", "name": audit_name, "on": "always", "format": "junit"},
            {"path": "reports/" + audit_name + ".json", "name": audit_name + "-detail", "on": "always"},
        ],
    )

def visual_diff(
        spec,
        base_url,
        browsers = ["chromium"],
        threshold = 0.01,
        after = [],
        image = "mcr.microsoft.com/playwright:v1.53.0-noble"):
    nodes = []
    for browser in browsers:
        if browser not in SUPPORTED_BROWSERS:
            fail("unsupported browser: " + browser)
        diff_name = sanitize_name(spec) + "-visual-" + browser
        node = test.run(
            name    = diff_name,
            file    = spec,
            image   = image,
            after   = after,
            env     = {
                "BROWSER":            browser,
                "BASE_URL":           base_url,
                "DIFF_THRESHOLD":     str(threshold),
                "PLAYWRIGHT_MODE":    "visual",
            },
            fail_on_logs = ["VISUAL_REGRESSION_DETECTED"],
            exports = [
                {"path": "snapshots/" + diff_name, "name": diff_name + "-snapshots", "on": "always"},
                {"path": "diffs/"     + diff_name, "name": diff_name + "-diffs",     "on": "failure"},
            ],
        )
        nodes.append(node)
    return nodes

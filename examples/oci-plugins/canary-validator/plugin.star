load("@babelsuite/runtime", "plugin")

_ref = plugin("canary-validator")

def validate(name, target, expected_ratio, canary_header = "X-Canary",
             tolerance = 0.05, sample_size = 100, severity = "critical", after = []):
    return _ref.validate(
        name           = name,
        after          = after,
        target         = target,
        expected_ratio = expected_ratio,
        canary_header  = canary_header,
        tolerance      = tolerance,
        sample_size    = sample_size,
        severity       = severity,
    )

def watch_ratio(name, target, expected_ratio, canary_header = "X-Canary",
                tolerance = 0.15, sample_size = 100, after = []):
    return _ref.watch_ratio(
        name           = name,
        after          = after,
        target         = target,
        expected_ratio = expected_ratio,
        canary_header  = canary_header,
        tolerance      = tolerance,
        sample_size    = sample_size,
    )

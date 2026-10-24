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

def sticky(name, target, session_header = "X-Session-Id", session_value = "babelsuite-sticky-check",
           canary_header = "X-Canary", sample_size = 20, severity = "critical", after = []):
    return _ref.sticky(
        name           = name,
        after          = after,
        target         = target,
        session_header = session_header,
        session_value  = session_value,
        canary_header  = canary_header,
        sample_size    = sample_size,
        severity       = severity,
    )

def compare_errors(name, target, max_error_delta = 0.01, canary_header = "X-Canary",
                   sample_size = 100, severity = "critical", after = []):
    return _ref.compare_errors(
        name            = name,
        after           = after,
        target          = target,
        max_error_delta = max_error_delta,
        canary_header   = canary_header,
        sample_size     = sample_size,
        severity        = severity,
    )

load("@babelsuite/runtime", "plugin")

_ref = plugin("shadow-diff")

def diff(name, primary, shadow, threshold = 0, ignore_fields = [],
         severity = "warn", after = []):
    return _ref.diff(
        name          = name,
        after         = after,
        primary       = primary,
        shadow        = shadow,
        threshold     = threshold,
        ignore_fields = ignore_fields,
        severity      = severity,
    )

def check(name, primary, shadow, threshold = 0, ignore_fields = [], after = []):
    return _ref.check(
        name          = name,
        after         = after,
        primary       = primary,
        shadow        = shadow,
        threshold     = threshold,
        ignore_fields = ignore_fields,
    )

def compare_paths(name, primary, shadow, paths, threshold = 0, ignore_fields = [],
                  severity = "warn", after = []):
    return _ref.compare_paths(
        name          = name,
        after         = after,
        primary       = primary,
        shadow        = shadow,
        paths         = paths,
        threshold     = threshold,
        ignore_fields = ignore_fields,
        severity      = severity,
    )

def compare_latency(name, primary, shadow, samples = 10, max_ratio = 1.5,
                    severity = "warn", after = []):
    return _ref.compare_latency(
        name      = name,
        after     = after,
        primary   = primary,
        shadow    = shadow,
        samples   = samples,
        max_ratio = max_ratio,
        severity  = severity,
    )

load("@babelsuite/runtime", "plugin")

_ref = plugin("shadow-diff")

def diff(name, primary, shadow, threshold = 0, ignore_fields = [],
         severity = "critical", after = [], **kw):
    return _ref.diff(
        name          = name,
        after         = after,
        primary       = primary,
        shadow        = shadow,
        threshold     = threshold,
        ignore_fields = ignore_fields,
        severity      = severity,
        **kw,
    )

def check(name, primary, shadow, threshold = 0, ignore_fields = [], after = [], **kw):
    return _ref.check(
        name          = name,
        after         = after,
        primary       = primary,
        shadow        = shadow,
        threshold     = threshold,
        ignore_fields = ignore_fields,
        **kw,
    )

def compare_paths(name, primary, shadow, paths, threshold = 0, ignore_fields = [],
                  severity = "critical", after = [], **kw):
    return _ref.compare_paths(
        name          = name,
        after         = after,
        primary       = primary,
        shadow        = shadow,
        paths         = paths,
        threshold     = threshold,
        ignore_fields = ignore_fields,
        severity      = severity,
        **kw,
    )

def compare_latency(name, primary, shadow, samples = 10, max_ratio = 1.5,
                    severity = "critical", after = [], **kw):
    return _ref.compare_latency(
        name      = name,
        after     = after,
        primary   = primary,
        shadow    = shadow,
        samples   = samples,
        max_ratio = max_ratio,
        severity  = severity,
        **kw,
    )

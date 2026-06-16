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

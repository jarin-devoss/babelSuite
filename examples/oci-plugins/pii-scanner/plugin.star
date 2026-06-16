load("@babelsuite/runtime", "plugin")

_ref = plugin("pii-scanner")

def scan(name, target, patterns = [], severity = "critical", after = []):
    return _ref.scan(
        name     = name,
        after    = after,
        target   = target,
        patterns = patterns,
        severity = severity,
    )

def probe(name, target, patterns = [], after = []):
    return _ref.probe(
        name     = name,
        after    = after,
        target   = target,
        patterns = patterns,
    )

load("@babelsuite/runtime", "plugin")

_ref = plugin("schema-compat")

def check_compat(name, registry_url, subject, new_schema,
                 mode = "BACKWARD", severity = "critical", after = []):
    return _ref.check_compat(
        name         = name,
        after        = after,
        registry_url = registry_url,
        subject      = subject,
        new_schema   = new_schema,
        mode         = mode,
        severity     = severity,
    )

def enforce(name, registry_url, subject, new_schema,
            severity = "critical", after = []):
    return _ref.enforce(
        name         = name,
        after        = after,
        registry_url = registry_url,
        subject      = subject,
        new_schema   = new_schema,
        severity     = severity,
    )

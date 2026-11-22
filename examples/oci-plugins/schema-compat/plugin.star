load("@babelsuite/runtime", "plugin")

_ref = plugin("schema-compat")

def check_compat(name, registry_url, subject, new_schema,
                 mode = "BACKWARD", severity = "critical", after = [], **kw):
    return _ref.check_compat(
        name         = name,
        after        = after,
        registry_url = registry_url,
        subject      = subject,
        new_schema   = new_schema,
        mode         = mode,
        severity     = severity,
        **kw,
    )

def enforce(name, registry_url, subject, new_schema,
            severity = "critical", after = [], **kw):
    return _ref.enforce(
        name         = name,
        after        = after,
        registry_url = registry_url,
        subject      = subject,
        new_schema   = new_schema,
        severity     = severity,
        **kw,
    )

def check_registered(name, registry_url, subject, new_schema,
                     severity = "critical", after = [], **kw):
    return _ref.check_registered(
        name         = name,
        after        = after,
        registry_url = registry_url,
        subject      = subject,
        new_schema   = new_schema,
        severity     = severity,
        **kw,
    )

def check_mode(name, registry_url, subject, mode = "BACKWARD",
               severity = "critical", after = [], **kw):
    return _ref.check_mode(
        name         = name,
        after        = after,
        registry_url = registry_url,
        subject      = subject,
        mode         = mode,
        severity     = severity,
        **kw,
    )

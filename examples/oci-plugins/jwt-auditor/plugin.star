load("@babelsuite/runtime", "plugin")

_ref = plugin("jwt-auditor")

def check_token(name, auth_url, username, password, token_field = "token",
                max_lifetime_hours = 24, required_claims = ["sub", "iss", "exp", "iat"],
                severity = "high", after = []):
    return _ref.check_token(
        name               = name,
        after              = after,
        auth_url           = auth_url,
        username           = username,
        password           = password,
        token_field        = token_field,
        max_lifetime_hours = max_lifetime_hours,
        required_claims    = required_claims,
        severity           = severity,
    )

def audit_endpoint(name, auth_url, username, password, endpoint,
                   token_field = "token", severity = "high", after = []):
    return _ref.audit_endpoint(
        name        = name,
        after       = after,
        auth_url    = auth_url,
        username    = username,
        password    = password,
        endpoint    = endpoint,
        token_field = token_field,
        severity    = severity,
    )

load("@babelsuite/runtime", "plugin")

_ref = plugin("llm-judge")

def judge(name, criteria, model_url, output = "", target = "", model = "gpt-4o-mini",
          api_key = "", threshold = 1.0, timeout_ms = 30000, severity = "critical", after = []):
    return _ref.judge(
        name       = name,
        after      = after,
        criteria   = criteria,
        model_url  = model_url,
        output     = output,
        target     = target,
        model      = model,
        api_key    = api_key,
        threshold  = threshold,
        timeout_ms = timeout_ms,
        severity   = severity,
    )

def monitor(name, criteria, model_url, output = "", target = "", model = "gpt-4o-mini",
            api_key = "", threshold = 1.0, timeout_ms = 30000, after = []):
    return _ref.monitor(
        name       = name,
        after      = after,
        criteria   = criteria,
        model_url  = model_url,
        output     = output,
        target     = target,
        model      = model,
        api_key    = api_key,
        threshold  = threshold,
        timeout_ms = timeout_ms,
    )

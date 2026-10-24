load("@babelsuite/runtime", "plugin")

_ref = plugin("llm-judge")

def judge(name, criteria, model_url, output = "", target = "", model = "gpt-4o-mini",
          api_key = "", threshold = 1.0, timeout_ms = 30000, severity = "critical",
          structured = True, after = []):
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
        structured = structured,
    )

def monitor(name, criteria, model_url, output = "", target = "", model = "gpt-4o-mini",
            api_key = "", threshold = 1.0, timeout_ms = 30000,
            structured = True, after = []):
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
        structured = structured,
    )

def consistency(name, criteria, model_url, output = "", target = "", model = "gpt-4o-mini",
                api_key = "", rounds = 3, max_spread = 0.0, timeout_ms = 30000,
                severity = "critical", structured = True, after = []):
    return _ref.consistency(
        name       = name,
        after      = after,
        criteria   = criteria,
        model_url  = model_url,
        output     = output,
        target     = target,
        model      = model,
        api_key    = api_key,
        rounds     = rounds,
        max_spread = max_spread,
        timeout_ms = timeout_ms,
        severity   = severity,
        structured = structured,
    )

def compare(name, criteria, model_url, output_a, output_b, prefer = "a",
            model = "gpt-4o-mini", api_key = "", timeout_ms = 30000,
            severity = "critical", structured = True, after = []):
    return _ref.compare(
        name       = name,
        after      = after,
        criteria   = criteria,
        model_url  = model_url,
        output_a   = output_a,
        output_b   = output_b,
        prefer     = prefer,
        model      = model,
        api_key    = api_key,
        timeout_ms = timeout_ms,
        severity   = severity,
        structured = structured,
    )

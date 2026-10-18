load("@babelsuite/runtime", "plugin")

_ref = plugin("skill-eval")

# Suite authors describe skills and cases as dicts, but plugin config only
# carries strings, numbers, bools and lists — a dict would reach the sidecar as
# its repr string. These helpers flatten both into parallel lists, which the
# Lua handler zips back together.
#
#   skills: [{"name", "description", "instructions"?, "parameters"? (JSON schema string)}]
#   cases:  [{"prompt", "expect"? (skill name, None = no skill), "args"? (dict),
#             "criteria"? (list), "sequence"? (skill names, in call order)}]

def _skill_config(skills):
    return {
        "skill_names":        [s["name"] for s in skills],
        "skill_descriptions": [s.get("description", "") for s in skills],
        "skill_instructions": [s.get("instructions", "") for s in skills],
        "skill_parameters":   [s.get("parameters", "") for s in skills],
    }

def _case_config(cases):
    return {
        "case_prompts":  [c["prompt"] for c in cases],
        "case_expects":  [c.get("expect") for c in cases],
        # args become [key, value] pairs; values may be strings, numbers,
        # bools or lists.
        "case_args":     [[[k, v] for k, v in c.get("args", {}).items()] for c in cases],
        "case_criteria": [c.get("criteria", []) for c in cases],
        # Empty means "just expect", or no call when expect is None.
        "case_sequences": [c.get("sequence", []) for c in cases],
    }

def _dispatch(op, name, after, skills, cases, **config):
    config.update(_skill_config(skills))
    config.update(_case_config(cases))
    return getattr(_ref, op)(name = name, after = after, **config)

def trigger(name, skills, cases, model_url, model = "gpt-4o-mini", api_key = "",
            system = "", threshold = 1.0, timeout_ms = 30000,
            severity = "critical", after = []):
    return _dispatch("trigger", name, after, skills, cases,
        model_url  = model_url,
        model      = model,
        api_key    = api_key,
        system     = system,
        threshold  = threshold,
        timeout_ms = timeout_ms,
        severity   = severity,
    )

def arguments(name, skills, cases, model_url, model = "gpt-4o-mini", api_key = "",
              system = "", check_schema = True, threshold = 1.0, timeout_ms = 30000,
              severity = "critical", after = []):
    return _dispatch("arguments", name, after, skills, cases,
        model_url    = model_url,
        model        = model,
        api_key      = api_key,
        system       = system,
        check_schema = check_schema,
        threshold    = threshold,
        timeout_ms   = timeout_ms,
        severity     = severity,
    )

def outcome(name, skills, cases, model_url, model = "gpt-4o-mini", api_key = "",
            system = "", judge_url = "", judge_model = "", judge_api_key = "",
            threshold = 1.0, timeout_ms = 30000, severity = "critical",
            structured = True, after = []):
    return _dispatch("outcome", name, after, skills, cases,
        model_url     = model_url,
        model         = model,
        api_key       = api_key,
        system        = system,
        judge_url     = judge_url,
        judge_model   = judge_model,
        judge_api_key = judge_api_key,
        threshold     = threshold,
        timeout_ms    = timeout_ms,
        severity      = severity,
        structured    = structured,
    )

def uplift(name, skills, cases, model_url, model = "gpt-4o-mini", api_key = "",
           system = "", judge_url = "", judge_model = "", judge_api_key = "",
           min_gain = 1, threshold = 1.0, timeout_ms = 30000, severity = "critical",
           structured = True, after = []):
    return _dispatch("uplift", name, after, skills, cases,
        model_url     = model_url,
        model         = model,
        api_key       = api_key,
        system        = system,
        judge_url     = judge_url,
        judge_model   = judge_model,
        judge_api_key = judge_api_key,
        min_gain      = min_gain,
        threshold     = threshold,
        timeout_ms    = timeout_ms,
        severity      = severity,
        structured    = structured,
    )

def consistency(name, skills, cases, model_url, model = "gpt-4o-mini", api_key = "",
                system = "", samples = 5, temperature = 0.7, threshold = 1.0,
                timeout_ms = 30000, severity = "critical", after = []):
    return _dispatch("consistency", name, after, skills, cases,
        model_url   = model_url,
        model       = model,
        api_key     = api_key,
        system      = system,
        samples     = samples,
        temperature = temperature,
        threshold   = threshold,
        timeout_ms  = timeout_ms,
        severity    = severity,
    )

def sequence(name, skills, cases, model_url, model = "gpt-4o-mini", api_key = "",
             system = "", sequence_match = "exact", threshold = 1.0, timeout_ms = 30000,
             severity = "critical", after = []):
    return _dispatch("sequence", name, after, skills, cases,
        model_url      = model_url,
        model          = model,
        api_key        = api_key,
        system         = system,
        sequence_match = sequence_match,
        threshold      = threshold,
        timeout_ms     = timeout_ms,
        severity       = severity,
    )

def latency(name, skills, cases, model_url, model = "gpt-4o-mini", api_key = "",
            system = "", max_latency_ms = 5000, threshold = 1.0, timeout_ms = 30000,
            severity = "critical", after = []):
    return _dispatch("latency", name, after, skills, cases,
        model_url      = model_url,
        model          = model,
        api_key        = api_key,
        system         = system,
        max_latency_ms = max_latency_ms,
        threshold      = threshold,
        timeout_ms     = timeout_ms,
        severity       = severity,
    )

def monitor(name, skills, cases, model_url, model = "gpt-4o-mini", api_key = "",
            system = "", threshold = 1.0, timeout_ms = 30000, after = []):
    return _dispatch("monitor", name, after, skills, cases,
        model_url  = model_url,
        model      = model,
        api_key    = api_key,
        system     = system,
        threshold  = threshold,
        timeout_ms = timeout_ms,
    )

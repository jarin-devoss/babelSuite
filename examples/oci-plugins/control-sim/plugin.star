load("@babelsuite/runtime", "plugin")

_ref = plugin("control-sim")

def step_response(name, numerator, denominator, time_end = 10,
                  max_settling_time = None, max_overshoot_pct = None,
                  min_dc_gain = None, max_dc_gain = None,
                  sim_url = "", severity = "critical", after = []):
    return _ref.step_response(
        name             = name,
        after            = after,
        numerator        = numerator,
        denominator      = denominator,
        time_end         = time_end,
        max_settling_time = max_settling_time,
        max_overshoot_pct = max_overshoot_pct,
        min_dc_gain      = min_dc_gain,
        max_dc_gain      = max_dc_gain,
        sim_url          = sim_url,
        severity         = severity,
    )

def monitor(name, numerator, denominator, time_end = 10,
            max_settling_time = None, max_overshoot_pct = None,
            sim_url = "", after = []):
    return _ref.monitor(
        name              = name,
        after             = after,
        numerator         = numerator,
        denominator       = denominator,
        time_end          = time_end,
        max_settling_time = max_settling_time,
        max_overshoot_pct = max_overshoot_pct,
        sim_url           = sim_url,
    )

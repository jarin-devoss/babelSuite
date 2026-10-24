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

def impulse_response(name, numerator, denominator, time_end = 10,
                     max_settling_time = None, max_residual = None,
                     sim_url = "", severity = "critical", after = []):
    return _ref.impulse_response(
        name              = name,
        after             = after,
        numerator         = numerator,
        denominator       = denominator,
        time_end          = time_end,
        max_settling_time = max_settling_time,
        max_residual      = max_residual,
        sim_url           = sim_url,
        severity          = severity,
    )

def frequency_response(name, numerator, denominator, time_end = 10,
                       min_gain_margin_db = None, min_phase_margin_deg = None,
                       sim_url = "", severity = "critical", after = []):
    return _ref.frequency_response(
        name                 = name,
        after                = after,
        numerator            = numerator,
        denominator          = denominator,
        time_end             = time_end,
        min_gain_margin_db   = min_gain_margin_db,
        min_phase_margin_deg = min_phase_margin_deg,
        sim_url              = sim_url,
        severity             = severity,
    )

load("@babelsuite/runtime", "plugin")

_ref = plugin("spice-sim")

def transient(name, netlist, probe_node = "out", step_time_ms = 0.1, end_time_ms = 10,
              max_rise_ms = None, max_voltage = None, min_voltage = None,
              sim_url = "", severity = "critical", after = []):
    return _ref.transient(
        name         = name,
        after        = after,
        netlist      = netlist,
        probe_node   = probe_node,
        step_time_ms = step_time_ms,
        end_time_ms  = end_time_ms,
        max_rise_ms  = max_rise_ms,
        max_voltage  = max_voltage,
        min_voltage  = min_voltage,
        sim_url      = sim_url,
        severity     = severity,
    )

def dc_sweep(name, netlist, probe_node = "out", max_voltage = None, min_voltage = None,
             sim_url = "", severity = "critical", after = []):
    return _ref.dc_sweep(
        name        = name,
        after       = after,
        netlist     = netlist,
        probe_node  = probe_node,
        max_voltage = max_voltage,
        min_voltage = min_voltage,
        sim_url     = sim_url,
        severity    = severity,
    )

def ac_analysis(name, netlist, probe_node = "out", min_gain_db = None,
                sim_url = "", severity = "critical", after = []):
    return _ref.ac_analysis(
        name        = name,
        after       = after,
        netlist     = netlist,
        probe_node  = probe_node,
        min_gain_db = min_gain_db,
        sim_url     = sim_url,
        severity    = severity,
    )

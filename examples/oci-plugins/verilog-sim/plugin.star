load("@babelsuite/runtime", "plugin")

_ref = plugin("verilog-sim")

def simulate(name, top_module, verilog, testbench, timeout_ns = 200,
             max_errors = 0, assert_no_x = True, sim_url = "", severity = "critical", after = []):
    return _ref.simulate(
        name        = name,
        after       = after,
        top_module  = top_module,
        verilog     = verilog,
        testbench   = testbench,
        timeout_ns  = timeout_ns,
        max_errors  = max_errors,
        assert_no_x = assert_no_x,
        sim_url     = sim_url,
        severity    = severity,
    )

def strict(name, top_module, verilog, testbench, timeout_ns = 200,
           sim_url = "", severity = "critical", after = []):
    return _ref.strict(
        name       = name,
        after      = after,
        top_module = top_module,
        verilog    = verilog,
        testbench  = testbench,
        timeout_ns = timeout_ns,
        sim_url    = sim_url,
        severity   = severity,
    )

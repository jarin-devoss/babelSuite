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

def lint(name, top_module, verilog, testbench = "", timeout_ns = 200,
         fail_on_warnings = False, sim_url = "", severity = "critical", after = []):
    return _ref.lint(
        name             = name,
        after            = after,
        top_module       = top_module,
        verilog          = verilog,
        testbench        = testbench,
        timeout_ns       = timeout_ns,
        fail_on_warnings = fail_on_warnings,
        sim_url          = sim_url,
        severity         = severity,
    )

def check_coverage(name, top_module, verilog, testbench, min_coverage_pct = 80,
                   timeout_ns = 200, sim_url = "", severity = "critical", after = []):
    return _ref.check_coverage(
        name             = name,
        after            = after,
        top_module       = top_module,
        verilog          = verilog,
        testbench        = testbench,
        min_coverage_pct = min_coverage_pct,
        timeout_ns       = timeout_ns,
        sim_url          = sim_url,
        severity         = severity,
    )

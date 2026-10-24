// Digital circuit simulation plugin config schema (Icarus Verilog backend).
// Validates configuration passed to plugin.run(plugin="babelsuite-verilog-sim", config={...}).

sim_url:        string          // HTTP URL of the Icarus Verilog simulation microservice
top_module:     string          // name of the top-level module to simulate
verilog:        string          // Verilog source for the design under test (.v content)
testbench?:     string          // Verilog testbench source (.v content, must call $finish); omit for lint
timeout_ns:     int & >0 | *200 // simulation time limit in nanoseconds
assert_no_x:    bool | *true    // fail if any output wire is X (undefined) at $finish
max_errors:     int & >=0 | *0  // max allowed $error/$fatal calls before failing
severity:       "warn" | "critical" | *"critical"

// lint: testbench is optional; promote compiler warnings to failures.
fail_on_warnings: bool | *false

// check_coverage: minimum toggle coverage the testbench must reach.
min_coverage_pct: number & >=0 & <=100 | *80

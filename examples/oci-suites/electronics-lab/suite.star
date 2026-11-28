load("@plugins/spice-sim",   "transient")
load("@plugins/verilog-sim", "simulate", "strict", "lint", "check_coverage")
load("@plugins/control-sim", "step_response")

# ── which design is sent ─────────────────────────────────────────────────────
# DESIGN is "good" by default. The broken.yaml profile sets it to "bad": the same
# checks then send a slow RC filter, a counter that never resets and a ringing
# plant, and the run must end Failed. The thresholds come from the profile as well
# (strict.yaml tightens them).
DESIGN        = env.get("DESIGN", "good")
MAX_RISE_MS   = float(env.get("MAX_RISE_MS",   "3.0"))
MAX_VOLTAGE   = float(env.get("MAX_VOLTAGE",   "5.5"))
MAX_SETTLING  = float(env.get("MAX_SETTLING_S", "8.0"))
MAX_OVERSHOOT = float(env.get("MAX_OVERSHOOT_PCT", "5.0"))
MIN_COVERAGE  = float(env.get("MIN_COVERAGE_PCT", "80"))

# The simulators are mocks (spice-service, verilog-service, control-service). Each
# answers by the design it is sent: an example per known design, an error for any
# other. They do not simulate; their answers are the ones written in mock/.
spice_mock   = mock.api(name="spice-service")
verilog_mock = mock.api(name="verilog-service")
control_mock = mock.api(name="control-service")

# ── the designs ──────────────────────────────────────────────────────────────
# R1 is 1k in the good filter (rise 2.2 ms) and 10k in the slow one (rise 22 ms).
RC_NETLIST = "RC Filter\nVinput 1 0 DC 5V\nR1 1 2 1k\nC1 2 0 1uF\n.tran 0.1ms 10ms\n.end"
if DESIGN == "bad":
    RC_NETLIST = "RC Filter\nVinput 1 0 DC 5V\nR1 1 2 10k\nC1 2 0 1uF\n.tran 1ms 100ms\n.end"

# The good counter resets to zero and counts; the bad one never assigns its reset.
COUNTER_VERILOG = "module counter(input wire clk, input wire rst, output reg [3:0] out);\n  always @(posedge clk or posedge rst) begin\n    if (rst) out <= 4'b0000; else out <= out + 1;\n  end\nendmodule"
if DESIGN == "bad":
    COUNTER_VERILOG = "module counter(input wire clk, input wire rst, output reg [3:0] out);\n  always @(posedge clk or posedge rst) begin\n    out <= out + 1;\n  end\nendmodule"

COUNTER_TESTBENCH = "module tb_counter;\n  reg clk, rst; wire [3:0] out; integer errors;\n  counter uut (.clk(clk), .rst(rst), .out(out));\n  always #5 clk = ~clk;\n  initial begin\n    clk=0; rst=1; errors=0; #10 rst=0; #10;\n    if (out !== 4'd1) begin $error(\"expected 1, got %d\", out); errors=errors+1; end\n    #10;\n    if (out !== 4'd2) begin $error(\"expected 2, got %d\", out); errors=errors+1; end\n    #80;\n    if (errors > 0) $fatal(1, \"%d assertion(s) failed\", errors);\n    $finish;\n  end\nendmodule"

# G(s) = 1/(s^2 + 2s + 1) is critically damped; 1/(s^2 + 0.2s + 1) rings.
DAMPER_DENOMINATOR = [1, 2, 1]
if DESIGN == "bad":
    DAMPER_DENOMINATOR = [1, 0.2, 1]

rc_filter = transient(
    name        = "rc-filter-step-response",
    netlist     = RC_NETLIST,
    probe_node  = "2",
    max_rise_ms = MAX_RISE_MS,
    max_voltage = MAX_VOLTAGE,
    sim_url     = "http://spice-service:9080",
    severity    = "critical",
    after       = [spice_mock],
)

counter_check = simulate(
    name       = "4bit-counter-correctness",
    top_module = "tb_counter",
    verilog    = COUNTER_VERILOG,
    testbench  = COUNTER_TESTBENCH,
    timeout_ns = 200,
    sim_url    = "http://verilog-service:9080",
    severity   = "critical",
    after      = [verilog_mock],
)

# strict allows nothing: any X, any error fails.
counter_strict = strict(
    name       = "4bit-counter-strict",
    top_module = "tb_counter",
    verilog    = COUNTER_VERILOG,
    testbench  = COUNTER_TESTBENCH,
    timeout_ns = 200,
    sim_url    = "http://verilog-service:9080",
    severity   = "critical",
    after      = [counter_check],
)

# lint compiles the design without running a testbench; fail_on_warnings makes
# its warnings gate.
counter_lint = lint(
    name             = "4bit-counter-lint",
    top_module       = "tb_counter",
    verilog          = COUNTER_VERILOG,
    fail_on_warnings = True,
    sim_url          = "http://verilog-service:9080",
    severity         = "critical",
    after            = [verilog_mock],
)

# check_coverage fails when the testbench leaves too much of the design unexercised.
counter_coverage = check_coverage(
    name             = "4bit-counter-coverage",
    top_module       = "tb_counter",
    verilog          = COUNTER_VERILOG,
    testbench        = COUNTER_TESTBENCH,
    min_coverage_pct = MIN_COVERAGE,
    sim_url          = "http://verilog-service:9080",
    severity         = "critical",
    after            = [verilog_mock],
)

damper = step_response(
    name              = "mass-spring-damper-response",
    numerator         = [1],
    denominator       = DAMPER_DENOMINATOR,
    time_end          = 10,
    max_settling_time = MAX_SETTLING,
    max_overshoot_pct = MAX_OVERSHOOT,
    min_dc_gain       = 0.9,
    max_dc_gain       = 1.1,
    sim_url           = "http://control-service:9080",
    severity          = "critical",
    after             = [control_mock],
)

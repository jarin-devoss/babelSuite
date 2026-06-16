load("@plugins/spice-sim",   "transient")
load("@plugins/verilog-sim", "simulate")
load("@plugins/control-sim", "monitor")

spice_mock   = service.mock(name="spice-service")
verilog_mock = service.mock(name="verilog-service")
control_mock = service.mock(name="control-service")

_counter_verilog = "\n".join([
    "module counter(input wire clk, input wire rst, output reg [3:0] out);",
    "  always @(posedge clk or posedge rst) begin",
    "    if (rst) out <= 4'b0000; else out <= out + 1;",
    "  end",
    "endmodule",
])

_counter_tb = "\n".join([
    "module tb_counter;",
    "  reg clk, rst; wire [3:0] out; integer errors;",
    "  counter uut (.clk(clk), .rst(rst), .out(out));",
    "  always #5 clk = ~clk;",
    "  initial begin",
    "    clk=0; rst=1; errors=0; #10 rst=0; #10;",
    "    if (out !== 4'd1) begin $error(\"expected 1, got %d\", out); errors=errors+1; end",
    "    #10;",
    "    if (out !== 4'd2) begin $error(\"expected 2, got %d\", out); errors=errors+1; end",
    "    #80;",
    "    if (errors > 0) $fatal(1, \"%d assertion(s) failed\", errors);",
    "    $finish;",
    "  end",
    "endmodule",
])

rc_filter = transient(
    name        = "rc-filter-step-response",
    netlist     = "RC Filter\nVinput 1 0 DC 5V\nR1 1 2 1k\nC1 2 0 1uF\n.tran 0.1ms 10ms\n.end",
    probe_node  = "2",
    max_rise_ms = 3.0,
    max_voltage = 5.5,
    severity    = "critical",
    after       = [spice_mock],
)

counter_check = simulate(
    name       = "4bit-counter-correctness",
    top_module = "tb_counter",
    verilog    = _counter_verilog,
    testbench  = _counter_tb,
    timeout_ns = 200,
    severity   = "critical",
    after      = [verilog_mock],
)

damper_watch = monitor(
    name        = "mass-spring-damper-response",
    numerator   = [1],
    denominator = [1, 2, 1],
    time_end    = 10,
    after       = [control_mock],
)

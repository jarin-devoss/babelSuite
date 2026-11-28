// Hand-written. The Verilog service answers for the two counter designs the suite sends: the good one
// resets to zero and counts, the other never assigns its reset, so its output stays X.
// Any other design gets the fallback error. simulate, strict, lint and check_coverage all
// post the design to this one operation.
"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "electronics-lab"
artifact: "mock/verilog-service/simulate.cue"
operationId: "verilog-simulate"
adapter: "rest"
dispatcher: "apisix"
contractPath: "api/verilog-service.yaml#/paths/~1simulate/post"
resolverUrl: "/internal/mock-data/electronics-lab/verilog-service/verilog-simulate"
runtimeUrl: "/mocks/rest/electronics-lab/verilog-service/simulate"
examples: {
  "counter-with-reset": {
    dispatch: [{"from":"body","param":"verilog","value":"module counter(input wire clk, input wire rst, output reg [3:0] out);\n  always @(posedge clk or posedge rst) begin\n    if (rst) out <= 4'b0000; else out <= out + 1;\n  end\nendmodule"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        top_module: "tb_counter"
      }
    }
    responseSchema: {
      status: "200"
      mediaType: "application/json"
      body: {
        compile_errors: []
        warnings:       []
        sim_errors:     0
        x_signals:      []
        toggle_coverage_pct: 92.5
        stdout:         "Time=0 | Reset=1 | Counter Output=0\nTime=10 | Reset=0 | Counter Output=0\nTime=20 | Reset=0 | Counter Output=1\nTime=30 | Reset=0 | Counter Output=2"
      }
    }
  }
  "counter-without-reset": {
    dispatch: [{"from":"body","param":"verilog","value":"module counter(input wire clk, input wire rst, output reg [3:0] out);\n  always @(posedge clk or posedge rst) begin\n    out <= out + 1;\n  end\nendmodule"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        top_module: "tb_counter"
      }
    }
    responseSchema: {
      status: "200"
      mediaType: "application/json"
      body: {
        compile_errors: []
        warnings:       ["rst is declared but never used in the counter"]
        sim_errors:     2
        x_signals:      ["out"]
        toggle_coverage_pct: 41.0
        stdout:         "Time=0 | Reset=1 | Counter Output=x\nTime=20 | Reset=0 | Counter Output=x\nERROR expected 1, got x\nERROR expected 2, got x"
      }
    }
  }
}

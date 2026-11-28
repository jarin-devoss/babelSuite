// Hand-written. The SPICE service answers for the two netlists the suite sends, each with the curve its
// RC time constant gives: 1k x 1uF rises 10-90% in 2.2 ms, 10k x 1uF in 22 ms. Any other
// netlist gets the fallback error.
"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "electronics-lab"
artifact: "mock/spice-service/simulate.cue"
operationId: "spice-simulate"
adapter: "rest"
dispatcher: "apisix"
contractPath: "api/spice-service.yaml#/paths/~1simulate/post"
resolverUrl: "/internal/mock-data/electronics-lab/spice-service/spice-simulate"
runtimeUrl: "/mocks/rest/electronics-lab/spice-service/simulate"
examples: {
  "rc-filter-1k": {
    dispatch: [{"from":"body","param":"netlist","value":"RC Filter\nVinput 1 0 DC 5V\nR1 1 2 1k\nC1 2 0 1uF\n.tran 0.1ms 10ms\n.end"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        analysis:   "transient"
        probe_node: "2"
      }
    }
    responseSchema: {
      status: "200"
      mediaType: "application/json"
      body: {
        times:    [0.0, 0.0005, 0.001, 0.0022, 0.003, 0.005, 0.007, 0.01]
        voltages: [0.0, 1.97, 3.16, 4.45, 4.75, 4.97, 5.0, 5.0]
        probe_node: "2"
      }
    }
  }
  "rc-filter-10k": {
    dispatch: [{"from":"body","param":"netlist","value":"RC Filter\nVinput 1 0 DC 5V\nR1 1 2 10k\nC1 2 0 1uF\n.tran 1ms 100ms\n.end"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        analysis:   "transient"
        probe_node: "2"
      }
    }
    responseSchema: {
      status: "200"
      mediaType: "application/json"
      body: {
        times:    [0.0, 0.005, 0.01, 0.022, 0.03, 0.05, 0.07, 0.1]
        voltages: [0.0, 1.97, 3.16, 4.45, 4.75, 4.97, 5.0, 5.0]
        probe_node: "2"
      }
    }
  }
}

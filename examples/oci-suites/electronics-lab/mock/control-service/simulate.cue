// Hand-written. The control service answers for the two plants the suite sends: 1/(s^2+2s+1) is critically
// damped (no overshoot, settles in about 4 s) and 1/(s^2+0.2s+1) rings (about 73% overshoot,
// settles in about 40 s). Any other plant gets the fallback error.
"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "electronics-lab"
artifact: "mock/control-service/simulate.cue"
operationId: "control-simulate"
adapter: "rest"
dispatcher: "apisix"
contractPath: "api/control-service.yaml#/paths/~1simulate/post"
resolverUrl: "/internal/mock-data/electronics-lab/control-service/control-simulate"
runtimeUrl: "/mocks/rest/electronics-lab/control-service/simulate"
examples: {
  "critically-damped": {
    dispatch: [{"from":"body","param":"denominator","value":"[1 2 1]"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        analysis: "step"
      }
    }
    responseSchema: {
      status: "200"
      mediaType: "application/json"
      body: {
        stable:        true
        settling_time: 4.1
        overshoot_pct: 0.0
        dc_gain:       1.0
      }
    }
  }
  "underdamped": {
    dispatch: [{"from":"body","param":"denominator","value":"[1 0.2 1]"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        analysis: "step"
      }
    }
    responseSchema: {
      status: "200"
      mediaType: "application/json"
      body: {
        stable:        true
        settling_time: 39.9
        overshoot_pct: 72.9
        dc_gain:       1.0
      }
    }
  }
}

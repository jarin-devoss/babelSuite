"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "fleet-control-room"
artifact: "mock/dispatch/get-fleet-status.cue"
operationId: "get-fleet-status"
adapter: "rest"
dispatcher: "apisix"
contractPath: "api/openapi/dispatcher.yaml#/paths/~1fleet~1status/get"
resolverUrl: "/internal/mock-data/fleet-control-room/dispatcher-api/get-fleet-status"
runtimeUrl: "/mocks/rest/fleet-control-room/dispatcher-api/fleet/status?scenario=live"
examples: {
  "live": {
    dispatch: [{"from":"query","param":"scenario","value":"live"}]
    requestSchema: {
      headers: {
        accept: "application/json"
      }
      body: null
    }
    responseSchema: {
      status: "200"
      mediaType: "application/json"
      headers: {
        "x-mock-source": "fleet-status"
      }
      body: {
        regions: 3
        status: "dispatching"
      }
    }
  }
}

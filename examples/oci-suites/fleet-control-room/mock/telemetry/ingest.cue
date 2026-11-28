// Hand-written. The telemetry service queues a frame, and refuses the one from vh-bad.
"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "fleet-control-room"
artifact: "mock/telemetry/ingest.cue"
operationId: "ingest-telemetry"
adapter: "rest"
dispatcher: "apisix"
contractPath: "api/openapi/fleet-services.yaml#/paths/~1telemetry~1events/post"
resolverUrl: "/internal/mock-data/fleet-control-room/fleet-services/ingest-telemetry"
runtimeUrl: "/mocks/rest/fleet-control-room/fleet-services/telemetry/events"
examples: {
  "refuses-bad-vehicle": {
    dispatch: [{"from":"body","param":"vehicleId","value":"vh-bad"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        vehicleId: "vh-bad"
        battery: -1
        speed: 0
      }
    }
    responseSchema: {
      status: "422"
      mediaType: "application/json"
      body: {
        accepted: false
        error: "frame refused"
      }
    }
  }
}

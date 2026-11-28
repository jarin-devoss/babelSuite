// Hand-written. The geo-fence service allows every zone except the airport apron.
"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "fleet-control-room"
artifact: "mock/geofence/check-zone.cue"
operationId: "check-zone"
adapter: "rest"
dispatcher: "apisix"
contractPath: "api/openapi/fleet-services.yaml#/paths/~1zones~1check/post"
resolverUrl: "/internal/mock-data/fleet-control-room/fleet-services/check-zone"
runtimeUrl: "/mocks/rest/fleet-control-room/fleet-services/zones/check"
examples: {
  "airport-apron-is-restricted": {
    dispatch: [{"from":"body","param":"zone","value":"airport-apron"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        zone: "airport-apron"
      }
    }
    responseSchema: {
      status: "200"
      mediaType: "application/json"
      body: {
        allowed: false
        reason: "restricted airspace"
      }
    }
  }
}

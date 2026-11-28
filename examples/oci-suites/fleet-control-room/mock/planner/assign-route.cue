// Hand-written. The route planner has a plan for vh-11 and none for any other vehicle (grpc-status 5, not found).
"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "fleet-control-room"
artifact: "mock/planner/assign-route.cue"
operationId: "assign-route"
adapter: "grpc"
dispatcher: "apisix"
contractPath: "api/proto/fleet_control.proto#AssignRoute"
resolverUrl: "/internal/mock-data/fleet-control-room/route-planner/assign-route"
runtimeUrl: "/mocks/grpc/fleet-control-room/route-planner/assign-route"
examples: {
  "plan-for-vh-11": {
    dispatch: [{"from":"body","param":"vehicleId","value":"vh-11"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        routeId: "route-778"
        vehicleId: "vh-11"
        zone: "depot-north"
      }
    }
    responseSchema: {
      status: "0"
      mediaType: "application/grpc"
      body: {
        status: "accepted"
        assignmentId: "asg-778"
        plannerRevision: "route-planner@4"
      }
    }
  }
}

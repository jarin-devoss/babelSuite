// Hand-written. The push provider accepts a message, and refuses the one sent to
// fail@example.test, which is how a suite makes a send fail on purpose.
"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "notification-hub"
artifact: "mock/push/send.cue"
operationId: "send-push"
adapter: "rest"
dispatcher: "apisix"
contractPath: "api/channels.yaml#/paths/~1push~1send/post"
resolverUrl: "/internal/mock-data/notification-hub/channel-providers/send-push"
runtimeUrl: "/mocks/rest/notification-hub/channel-providers/push/send"
examples: {
  "provider-refuses": {
    dispatch: [{"from":"body","param":"recipient","value":"fail@example.test"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        recipient: "fail@example.test"
        subject: "any"
        body: "any"
      }
    }
    responseSchema: {
      status: "503"
      mediaType: "application/json"
      body: {
        error: "the push provider is unavailable"
      }
    }
  }
}

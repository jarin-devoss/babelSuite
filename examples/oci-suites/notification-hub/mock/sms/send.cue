// Hand-written. The sms provider accepts a message, and refuses the one sent to
// fail@example.test, which is how a suite makes a send fail on purpose.
"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "notification-hub"
artifact: "mock/sms/send.cue"
operationId: "send-sms"
adapter: "rest"
dispatcher: "apisix"
contractPath: "api/channels.yaml#/paths/~1sms~1send/post"
resolverUrl: "/internal/mock-data/notification-hub/channel-providers/send-sms"
runtimeUrl: "/mocks/rest/notification-hub/channel-providers/sms/send"
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
        error: "the sms provider is unavailable"
      }
    }
  }
}

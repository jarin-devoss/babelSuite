// Fixture mock for the UI end-to-end tests.
"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "ui-mock"
artifact: "mock/greeting/get-greeting.cue"
operationId: "get-greeting"
adapter: "rest"
dispatcher: "apisix"
contractPath: "api/openapi/greeting.yaml#/paths/~1greeting/get"
resolverUrl: "/internal/mock-data/ui-mock/ui-api/get-greeting"
runtimeUrl: "/mocks/rest/ui-mock/ui-api/greeting?scenario=hello"
examples: {
  "hello": {
    dispatch: [{"from":"query","param":"scenario","value":"hello"}]
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
        "x-mock-source": "ui-mock"
      }
      body: {
        message: "hello from ui-mock"
      }
    }
  }
}

// Hand-written. The catalog takes an order, except for sku_4040, which is sold out.
// It counts the orders of each shopper (previousAttempts), per run.
"$schema": "https://schemas.babelsuite.dev/mock-exchange-source-v1.json"
suite: "storefront-browser-lab"
artifact: "mock/orders/create-order.cue"
operationId: "create-order"
adapter: "rest"
dispatcher: "apisix"
contractPath: "api/openapi/orders.yaml#/paths/~1orders/post"
resolverUrl: "/internal/mock-data/storefront-browser-lab/storefront-api/create-order"
runtimeUrl: "/mocks/rest/storefront-browser-lab/storefront-api/orders"
examples: {
  "out-of-stock": {
    dispatch: [{"from":"body","param":"sku","value":"sku_4040"}]
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        email: "shopper@demo.test"
        quantity: 1
        sku: "sku_4040"
      }
    }
    responseSchema: {
      status: "409"
      mediaType: "application/json"
      body: {
        code: "OUT_OF_STOCK"
        message: "Requested item is not available."
      }
    }
  }
  "standard-order": {
    dispatch: []
    requestSchema: {
      headers: {
        "content-type": "application/json"
      }
      body: {
        email: "shopper@demo.test"
        quantity: 1
        sku: "sku_1001"
      }
    }
    responseSchema: {
      status: "201"
      mediaType: "application/json"
      body: {
        status: "accepted"
        orderId: "ord_7001"
        previousAttempts: string @resolve(path="state.count")
      }
    }
  }
}

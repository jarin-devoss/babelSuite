"""The storefront storefront-browser-lab tests.

A small web shop: a page that lists the catalog's products, lets a shopper put
them in a cart and check out, and a JSON API behind it. The catalog (products and
orders) is a mock, reached by its name; the browser tests drive the page.

  GET  /                the shop page
  GET  /api/products    the catalog's products (from catalog-api)
  POST /api/orders      {email, sku, quantity}  201 {orderId} | 409 out of stock | 400 | 502
  GET  /health

The rule the suite exists to protect: an order the catalog refuses is never
confirmed to the shopper (see place_order()). Take that out and api-smoke, which
orders the sold-out item, fails, and so does the browser test that checks out with
it.
"""
import json
import os
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

CATALOG = os.environ.get("CATALOG_URL", "http://catalog-api:9080").rstrip("/")

PAGE = """<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Storefront Browser Lab</title></head>
<body>
<h1>Storefront Browser Lab</h1>
<ul id="grid" aria-label="Products"></ul>
<section aria-label="Cart">
  <h2>Cart</h2>
  <ul id="cart"></ul>
  <p id="total">Total: $0.00</p>
  <button id="checkout" disabled>Check out</button>
</section>
<p id="result" role="status"></p>
<script>
const money = (cents) => "$" + (cents / 100).toFixed(2);
const cart = [];
const grid = document.getElementById("grid");
const list = document.getElementById("cart");
const checkout = document.getElementById("checkout");
const result = document.getElementById("result");

function draw() {
  list.innerHTML = "";
  let total = 0;
  for (const line of cart) {
    total += line.price;
    const item = document.createElement("li");
    item.textContent = line.name + " " + money(line.price);
    list.appendChild(item);
  }
  document.getElementById("total").textContent = "Total: " + money(total);
  checkout.disabled = cart.length === 0;
}

fetch("/api/products").then((r) => r.json()).then((body) => {
  for (const product of body.products) {
    const item = document.createElement("li");
    item.dataset.sku = product.sku;
    item.append(product.name + " " + money(product.price) + " ");
    const add = document.createElement("button");
    add.textContent = "Add to cart";
    add.setAttribute("aria-label", "Add " + product.name);
    add.onclick = () => { cart.push(product); result.textContent = ""; draw(); };
    item.appendChild(add);
    grid.appendChild(item);
  }
});

document.getElementById("checkout").onclick = async () => {
  for (const line of cart) {
    const reply = await fetch("/api/orders", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "shopper@demo.test", sku: line.sku, quantity: 1 }),
    });
    const body = await reply.json();
    if (!reply.ok) { result.textContent = line.name + ": " + (body.error || "the order failed"); return; }
    result.textContent = "Order confirmed " + body.orderId;
  }
  cart.length = 0;
  draw();
};
</script>
</body>
</html>
"""


def catalog(method, path, body=None):
    request = urllib.request.Request(CATALOG + path, method=method, data=None if body is None else json.dumps(body).encode(),
                                     headers={"Content-Type": "application/json", "Accept": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=10) as reply:
            return reply.status, json.loads(reply.read() or b"{}")
    except urllib.error.HTTPError as err:
        try:
            return err.code, json.loads(err.read() or b"{}")
        except ValueError:
            return err.code, {}
    except (urllib.error.URLError, OSError, ValueError):
        return 0, {}


def place_order(body):
    if not isinstance(body, dict) or not body.get("sku") or not isinstance(body.get("quantity"), int) or body["quantity"] < 1 or "@" not in str(body.get("email", "")):
        return 400, {"error": "email, sku and a quantity of at least 1 are required"}
    status, answer = catalog("POST", "/orders", {"email": body["email"], "sku": body["sku"], "quantity": body["quantity"]})
    if status == 409:  # the rule: an order the catalog refuses is not confirmed
        print("ORDER_REFUSED %s" % body["sku"], flush=True)
        return 409, {"error": "Out of stock", "code": answer.get("code", "")}
    if status not in (200, 201) or not answer.get("orderId"):
        return 502, {"error": "the catalog did not take the order"}
    return 201, {"orderId": answer["orderId"], "status": answer.get("status", "accepted")}


class Server(ThreadingHTTPServer):
    request_queue_size = 256
    daemon_threads = True


class Handler(BaseHTTPRequestHandler):
    server_version = "storefront"

    def send(self, status, payload, kind="application/json"):
        data = payload if isinstance(payload, bytes) else json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/health":
            return self.send(200, {"status": "ok"})
        if self.path in ("/", "/index.html"):
            return self.send(200, PAGE.encode(), "text/html; charset=utf-8")
        if self.path == "/api/products":
            status, answer = catalog("GET", "/catalog/products?scenario=promo")
            if status != 200:
                return self.send(502, {"error": "the catalog did not answer"})
            return self.send(200, answer)
        self.send(404, {"error": "not found"})

    def do_POST(self):
        try:
            body = json.loads(self.rfile.read(int(self.headers.get("Content-Length") or 0)) or b"null")
        except ValueError:
            body = None
        if self.path == "/api/orders":
            status, answer = place_order(body)
            return self.send(status, answer)
        self.send(404, {"error": "not found"})

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    print("storefront on %d, catalog %s" % (port, CATALOG), flush=True)
    Server(("0.0.0.0", port), Handler).serve_forever()

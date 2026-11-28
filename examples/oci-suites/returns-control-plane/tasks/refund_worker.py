"""The refund service returns-control-plane tests.

It takes a return, asks the pricing service for a refund quote over gRPC (the mock
the gateway serves; the call is gRPC-Web, framed by hand because the standard
library has no gRPC client), registers the return with the partner (a mock, by its
name), and keeps what came of it.

  POST /returns                  {orderId, amountCents, currency?, returnReason}
                                 201 approved | 202 manual_review | 502 no quote
  POST /returns/{id}/approve     a reviewer approves a return held for review
  GET  /returns/{id}
  GET  /health

The rule the suite exists to protect: a refund above REFUND_LIMIT_CENTS is never
issued without manual review, whatever the pricing service says (see decide()).
Take that guard out and returns-smoke, which asks for a refund above the limit on
a reason the pricing mock calls "instant", fails.
"""
import json
import os
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

GATEWAY = os.environ.get("GATEWAY_URL", "http://babelsuite-gateway:9080").rstrip("/")
PARTNER = os.environ.get("PARTNER_URL", "http://partner-api:9080").rstrip("/")
LIMIT = int(os.environ.get("REFUND_LIMIT_CENTS", "10000"))
METHOD = "/returns_control_plane.v1.RefundPricing/QuoteRefund"

lock = threading.Lock()
returns = {}
sequence = [0]


# ── protobuf and gRPC-Web, by hand ──────────────────────────────────────────
def varint(number):
    out = b""
    while True:
        low, number = number & 0x7F, number >> 7
        out += bytes([low | 0x80 if number else low])
        if not number:
            return out


def encode_request(amount, currency, reason):
    message = varint(1 << 3) + varint(amount)
    for number, text in ((2, currency), (3, reason)):
        raw = text.encode()
        message += varint(number << 3 | 2) + varint(len(raw)) + raw
    return b"\x00" + len(message).to_bytes(4, "big") + message


def read_varint(data, at):
    shift = value = 0
    while True:
        byte = data[at]
        at += 1
        value |= (byte & 0x7F) << shift
        if not byte & 0x80:
            return value, at
        shift += 7


def decode_response(message):
    out, at = {}, 0
    while at < len(message):
        tag, at = read_varint(message, at)
        number, wire = tag >> 3, tag & 7
        if wire == 0:
            out[number], at = read_varint(message, at)
        elif wire == 2:
            size, at = read_varint(message, at)
            out[number] = message[at:at + size].decode()
            at += size
        else:
            raise ValueError("unexpected wire type %d" % wire)
    return {"decision": out.get(1, ""), "quoteId": out.get(2, ""), "refundCents": out.get(3, 0)}


def quote(amount, currency, reason):
    """The pricing service's quote, or (None, why) when it gave none."""
    request = urllib.request.Request(
        GATEWAY + METHOD, data=encode_request(amount, currency, reason), method="POST",
        headers={"Content-Type": "application/grpc-web+proto", "X-Grpc-Web": "1", "Accept": "application/grpc-web+proto"})
    try:
        with urllib.request.urlopen(request, timeout=10) as reply:
            body, header_status = reply.read(), reply.headers.get("grpc-status")
    except (urllib.error.URLError, OSError) as err:
        return None, "the pricing service did not answer: %s" % err
    # A call that fails before it has a message answers with its status in the
    # headers and no body (trailers-only); otherwise the status is in the trailers.
    message, status, at = None, header_status or "0", 0
    while at + 5 <= len(body):
        flag, size = body[at], int.from_bytes(body[at + 1:at + 5], "big")
        payload = body[at + 5:at + 5 + size]
        at += 5 + size
        if flag & 0x80:
            for line in payload.decode(errors="replace").split("\r\n"):
                if line.lower().startswith("grpc-status:"):
                    status = line.split(":", 1)[1].strip()
        else:
            message = payload
    if status != "0" or message is None:
        return None, "the pricing service refused the quote (grpc-status %s)" % status
    return decode_response(message), ""


# ── the rule ────────────────────────────────────────────────────────────────
def decide(answer, amount):
    """instant, unless the refund is above the limit: that needs a person."""
    if amount > LIMIT:  # the rule: no unreviewed refund above the limit
        return "manual_review"
    return "instant" if answer["decision"] == "instant" else "manual_review"


def register_with_partner(order_id, amount, reason, decision):
    scenario = "approved" if decision == "instant" else "manual-review"
    request = urllib.request.Request(
        "%s/returns?scenario=%s" % (PARTNER, scenario), method="POST",
        data=json.dumps({"orderId": order_id, "amountCents": amount, "returnReason": reason}).encode(),
        headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=10) as reply:
            return json.loads(reply.read()).get("returnId", "")
    except (urllib.error.URLError, OSError, ValueError):
        return ""


def create_return(body):
    if not isinstance(body, dict) or not body.get("orderId") or not isinstance(body.get("amountCents"), int) or body["amountCents"] <= 0:
        return 400, {"error": "orderId and a positive amountCents are required"}
    reason, currency = str(body.get("returnReason", "")), str(body.get("currency", "USD"))
    answer, why = quote(body["amountCents"], currency, reason)
    if answer is None:
        print("QUOTE_FAILED " + why, flush=True)
        return 502, {"error": why}
    decision = decide(answer, body["amountCents"])
    partner_id = register_with_partner(body["orderId"], body["amountCents"], reason, decision)
    if not partner_id:
        print("PARTNER_FAILED return of %s was not registered" % body["orderId"], flush=True)
        return 502, {"error": "the partner did not register the return"}
    with lock:
        sequence[0] += 1
        record = {"returnId": "ret_%d" % sequence[0], "orderId": body["orderId"], "partnerId": partner_id, "amountCents": body["amountCents"],
                  "status": "approved" if decision == "instant" else "manual_review",
                  "refundCents": answer["refundCents"] if decision == "instant" else 0, "quoteId": answer["quoteId"]}
        returns[record["returnId"]] = record
    return (201 if decision == "instant" else 202), record


class Server(ThreadingHTTPServer):
    request_queue_size = 256
    daemon_threads = True


class Handler(BaseHTTPRequestHandler):
    server_version = "refund-worker"

    def reply(self, status, body):
        payload = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def read_json(self):
        length = int(self.headers.get("Content-Length") or 0)
        try:
            return json.loads(self.rfile.read(length) or b"null")
        except ValueError:
            return None

    def do_GET(self):
        path = self.path.split("?")[0]
        if path == "/health":
            return self.reply(200, {"status": "ok"})
        if path.startswith("/returns/"):
            with lock:
                record = returns.get(path.rsplit("/", 1)[1])
            return self.reply(200, record) if record else self.reply(404, {"error": "no such return"})
        self.reply(404, {"error": "not found"})

    def do_POST(self):
        path = self.path.split("?")[0]
        if path == "/returns":
            return self.reply(*create_return(self.read_json()))
        if path.startswith("/returns/") and path.endswith("/approve"):
            with lock:
                record = returns.get(path.split("/")[2])
                if record is None:
                    return self.reply(404, {"error": "no such return"})
                if record["status"] != "manual_review":
                    return self.reply(409, {"error": "only a return held for review can be approved"})
                record["status"] = "approved"
                record["refundCents"] = record["amountCents"]
                return self.reply(200, dict(record))
        self.reply(404, {"error": "not found"})

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    print("refund-worker on %d: refund limit %d cents, pricing via %s" % (port, LIMIT, GATEWAY), flush=True)
    Server(("0.0.0.0", port), Handler).serve_forever()

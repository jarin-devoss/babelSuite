"""The payment service payment-suite tests.

It takes a payment, asks the card processor (the Stripe mock) to authorize it, and
keeps what comes of it: the payment, its refunds, and a ledger that a
reconciliation can check them against.

  POST /payments?...       {amount, currency, cardToken, merchantId}
                           201 authorized | 202 pending_review | 402 declined | 400 invalid
  GET  /payments/{id}      the payment, with what has been refunded
  GET  /payments?currency= every payment in a currency
  POST /refunds            {paymentId, amount}; 422 when more than is left
  GET  /ledger?currency=   {currency, entries, balance}
  GET  /health

The rule the suite exists to protect: a payment is posted to the ledger only when
the processor authorized it (AUTHORIZED = 201). Change that and the reconcile
steps, which compare the ledger with the payments, fail; so does checkout-smoke,
which expects a payment parked for review to leave the ledger untouched.

FRAUD_STRATEGY comes from the profile. With "strict" a payment the processor
parks for review is declined (402, FRAUD_BLOCK) instead of parked.
"""
import json
import os
import sys
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

PROCESSOR = os.environ.get("STRIPE_URL", "http://stripe-mock:9080").rstrip("/")
STRATEGY = os.environ.get("FRAUD_STRATEGY", "standard")
AUTHORIZED = 201  # the processor's status for an authorization
REVIEW = 202  # the processor parked it for review

lock = threading.Lock()
payments = {}
ledger = []  # {currency, amount, paymentId, kind}
sequence = [0]


def ask_processor(body, scenario):
    request = urllib.request.Request(
        PROCESSOR + "/payments?status=" + scenario,
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=10) as reply:
            return reply.status, json.loads(reply.read() or b"{}")
    except urllib.error.HTTPError as err:
        return err.code, {}
    except (urllib.error.URLError, OSError, ValueError) as err:
        return 0, {"error": str(err)}


def valid(body):
    if not isinstance(body, dict):
        return "the body must be a JSON object"
    if not isinstance(body.get("amount"), int) or body["amount"] <= 0:
        return "amount must be a positive integer (cents)"
    currency = body.get("currency")
    if not isinstance(currency, str) or len(currency) != 3 or not currency.isalpha():
        return "currency must be a 3-letter code"
    if not body.get("cardToken") or not body.get("merchantId"):
        return "cardToken and merchantId are required"
    return None


def create_payment(body):
    problem = valid(body)
    if problem:
        return 400, {"error": problem}
    # The processor decides by the card: a risky token is parked for review.
    scenario = "review" if body["cardToken"] == "tok_risky" else "approved"
    code, answer = ask_processor(body, scenario)
    if code not in (AUTHORIZED, REVIEW):
        return 502, {"error": "the processor did not answer a payment", "processorStatus": code}

    with lock:
        sequence[0] += 1
        payment_id = "pay_%d" % sequence[0]
        status = "authorized" if code == AUTHORIZED else "pending_review"
        if status == "pending_review" and STRATEGY == "strict":
            print("FRAUD_BLOCK payment %s declined: strict fraud strategy" % payment_id, flush=True)
            return 402, {"status": "declined", "reason": "strict-fraud-strategy"}
        payments[payment_id] = {
            "paymentId": payment_id, "processorId": answer.get("paymentId", ""), "status": status,
            "amount": body["amount"], "currency": body["currency"].upper(),
            "merchantId": body["merchantId"], "refunded": 0,
        }
        if code == AUTHORIZED:  # the rule: only an authorized payment reaches the ledger
            ledger.append({"currency": body["currency"].upper(), "amount": body["amount"], "paymentId": payment_id, "kind": "capture"})
        return (201 if status == "authorized" else 202), dict(payments[payment_id])


def create_refund(body):
    if not isinstance(body, dict) or not isinstance(body.get("amount"), int) or body["amount"] <= 0:
        return 400, {"error": "amount must be a positive integer (cents)"}
    with lock:
        payment = payments.get(body.get("paymentId"))
        if payment is None:
            return 404, {"error": "no such payment"}
        if payment["status"] != "authorized":
            return 422, {"error": "only an authorized payment can be refunded"}
        if body["amount"] > payment["amount"] - payment["refunded"]:
            return 422, {"error": "more than is left to refund", "left": payment["amount"] - payment["refunded"]}
        payment["refunded"] += body["amount"]
        ledger.append({"currency": payment["currency"], "amount": -body["amount"], "paymentId": payment["paymentId"], "kind": "refund"})
        return 201, {"paymentId": payment["paymentId"], "refunded": payment["refunded"]}


class Server(ThreadingHTTPServer):
    # A burst of connections (a load test, a rate-limit probe) must queue, not be reset.
    request_queue_size = 256
    daemon_threads = True


class Handler(BaseHTTPRequestHandler):
    server_version = "payment-api"

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
        url = urlparse(self.path)
        query = parse_qs(url.query)
        currency = (query.get("currency") or [""])[0].upper()
        if url.path == "/health":
            return self.reply(200, {"status": "ok"})
        with lock:
            if url.path == "/ledger":
                entries = [e for e in ledger if not currency or e["currency"] == currency]
                return self.reply(200, {"currency": currency, "entries": len(entries), "balance": sum(e["amount"] for e in entries)})
            if url.path == "/payments":
                return self.reply(200, {"payments": [p for p in payments.values() if not currency or p["currency"] == currency]})
            if url.path.startswith("/payments/"):
                payment = payments.get(url.path.rsplit("/", 1)[1])
                return self.reply(200, payment) if payment else self.reply(404, {"error": "no such payment"})
        self.reply(404, {"error": "not found"})

    def do_POST(self):
        path = urlparse(self.path).path
        if path == "/payments":
            return self.reply(*create_payment(self.read_json()))
        if path == "/refunds":
            return self.reply(*create_refund(self.read_json()))
        self.reply(404, {"error": "not found"})

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    print("payment-api listening on %d, processor %s, fraud strategy %s" % (port, PROCESSOR, STRATEGY), flush=True)
    Server(("0.0.0.0", port), Handler).serve_forever()
    sys.exit(0)

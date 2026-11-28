"""The notification service notification-hub tests.

It renders a template for a locale and sends it to a channel provider (a mock, by
its name), keeps what was delivered, and dead-letters what a provider would not
take. It only talks to a caller the identity broker vouches for, and it announces
payment events it has checked against the payment service.

  POST /api/v1/notifications    {channel, template, locale?, recipient, vars}
                                201 delivered | 202 dead-lettered | 401 | 422 | 429 | 502
  GET  /api/v1/notifications/{id}
  GET  /delivered?channel=      what was delivered
  GET  /dlq                     what was dead-lettered, with the error and attempts
  POST /payment-events          {eventType, paymentId}
  GET  /health

Callers authenticate with a session from the identity broker (broker-api, imported
suite identity-broker): the service asks it who the bearer token belongs to.
Payment events are checked against the payment service (payment-api, imported suite
payment-suite): an event the payment does not bear out is dropped, not announced.

The rule the suite exists to protect: a send a provider refuses is dead-lettered
(see dead_letter()), never dropped. Take that out and dlq-drain-audit, which counts
what was sent against what was delivered plus dead-lettered, fails.
"""
import json
import os
import threading
import time
import urllib.error
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

CHANNELS = [c for c in os.environ.get("CHANNELS", "email,sms,push,webhook").split(",") if c]
LOCALES = [c for c in os.environ.get("LOCALES", "en,de,fr,ja").split(",") if c]
RETRY_POLICY = os.environ.get("RETRY_POLICY", "exponential")  # linear | exponential | none
ENABLE_DLQ = os.environ.get("ENABLE_DLQ", "true") == "true"
RATE_LIMIT = float(os.environ.get("RATE_LIMIT_RPS", "0")) if os.environ.get("ENABLE_RATE_LIMIT", "true") == "true" else 0
BROKER_URL = os.environ.get("BROKER_URL", "http://broker-api:8080").rstrip("/")
PAYMENTS_URL = os.environ.get("PAYMENTS_URL", "http://payment-api:8080").rstrip("/")
ATTEMPTS = {"none": 1, "linear": 3, "exponential": 3}.get(RETRY_POLICY, 3)

CATEGORIES = ["transactional", "marketing", "alert", "digest"]
# One template per category and locale: "<category>_<locale>".
GREETINGS = {"en": "Hello", "de": "Hallo", "fr": "Bonjour", "ja": "Konnichiwa"}
TEMPLATES = {
    "%s_%s" % (category, locale): {"subject": "%s: {{topic}}" % category.title(), "body": "%s {{name}}, %s update: {{topic}}" % (greeting, category)}
    for category in CATEGORIES for locale, greeting in GREETINGS.items()
}
TEMPLATES["payment_event"] = {"subject": "Payment {{paymentId}}: {{eventType}}", "body": "Payment {{paymentId}} ({{amount}} {{currency}}) {{eventType}}"}

lock = threading.Lock()
delivered = []
dead = []
buckets = {}


def render(template, variables):
    out = {}
    for field in ("subject", "body"):
        text = template[field]
        for key, value in variables.items():
            text = text.replace("{{%s}}" % key, str(value))
        if "{{" in text:
            raise ValueError("a variable of the template was not given: " + text)
        out[field] = text
    return out


def http(method, url, body=None, headers=None, timeout=10):
    request = urllib.request.Request(url, method=method, data=None if body is None else json.dumps(body).encode(),
                                     headers=dict({"Content-Type": "application/json"}, **(headers or {})))
    try:
        with urllib.request.urlopen(request, timeout=timeout) as reply:
            return reply.status, json.loads(reply.read() or b"{}")
    except urllib.error.HTTPError as err:
        try:
            return err.code, json.loads(err.read() or b"{}")
        except ValueError:
            return err.code, {}
    except (urllib.error.URLError, OSError, ValueError):
        return 0, {}


def who(headers):
    """The email the identity broker says the bearer token belongs to, or None."""
    status, answer = http("GET", BROKER_URL + "/session", headers={"Authorization": headers.get("Authorization", "")})
    return answer.get("email") if status == 200 else None


def allowed(sender):
    if RATE_LIMIT <= 0:
        return True
    now = time.time()
    with lock:
        tokens, stamp = buckets.get(sender, (RATE_LIMIT, now))
        tokens = min(RATE_LIMIT, tokens + (now - stamp) * RATE_LIMIT)
        if tokens < 1:
            buckets[sender] = (tokens, now)
            return False
        buckets[sender] = (tokens - 1, now)
        return True


def send_to_provider(channel, recipient, rendered):
    """Sends with the retry policy. Returns (delivered, attempts, last error)."""
    error = ""
    for attempt in range(1, ATTEMPTS + 1):
        status, answer = http("POST", "http://%s-provider-mock:9080/%s/send" % (channel, channel),
                              {"recipient": recipient, **rendered})
        if status == 200:
            return True, attempt, ""
        error = "provider answered %s %s" % (status, answer.get("error", ""))
        if status and status < 500:
            break  # a refusal is not retried
        if attempt < ATTEMPTS:
            time.sleep(0.05 * (2 ** (attempt - 1) if RETRY_POLICY == "exponential" else 1))
    return False, attempt, error


def dead_letter(record, error, attempts):
    """A send a provider refuses is kept, with why, for someone to look at."""
    with lock:
        dead.append(dict(record, error=error, attempts=attempts))


def notify(body, sender):
    if not isinstance(body, dict):
        return 400, {"error": "the body must be a JSON object"}
    channel, name, recipient = body.get("channel"), body.get("template"), body.get("recipient")
    if channel not in CHANNELS:
        return 400, {"error": "unknown channel", "channels": CHANNELS}
    if not recipient or "@" not in str(recipient) and channel in ("email",):
        return 400, {"error": "a recipient is required"}
    locale = body.get("locale", "en")
    template = TEMPLATES.get(name) or TEMPLATES.get("%s_%s" % (name, locale))
    if template is None:
        return 422, {"error": "unknown template or locale", "template": name, "locale": locale}
    if not allowed(sender):
        return 429, {"error": "rate limit exceeded"}
    try:
        rendered = render(template, body.get("vars") or {})
    except ValueError as err:
        return 422, {"error": str(err)}
    record = {"id": str(uuid.uuid4()), "channel": channel, "recipient": recipient, "template": name, "locale": locale, **rendered}
    ok, attempts, error = send_to_provider(channel, recipient, rendered)
    record["attempts"] = attempts
    if ok:
        with lock:
            delivered.append(record)
        return 201, dict(record, status="sent")
    if ENABLE_DLQ:
        dead_letter(record, error, attempts)
        return 202, dict(record, status="dead-lettered")
    return 502, {"error": error}


def payment_event(body, sender):
    event, payment_id = (body or {}).get("eventType"), (body or {}).get("paymentId")
    status, payment = http("GET", "%s/payments/%s" % (PAYMENTS_URL, payment_id))
    if status != 200:
        print("EVENT_DROPPED payment %s is not known to the payment service" % payment_id, flush=True)
        return 422, {"error": "unknown payment"}
    bears_out = {"charge.succeeded": payment["status"] == "authorized", "charge.failed": payment["status"] != "authorized",
                 "refund.created": payment.get("refunded", 0) > 0, "dispute.opened": payment["status"] == "authorized"}
    if event not in bears_out:
        return 400, {"error": "unknown event type"}
    if not bears_out[event]:
        print("EVENT_DROPPED %s does not match payment %s (%s)" % (event, payment_id, payment["status"]), flush=True)
        return 409, {"error": "the payment does not bear this event out"}
    if "email" not in CHANNELS:
        print("ROUTING_FAILED no email channel for %s" % event, flush=True)
        return 503, {"error": "no channel to route the event to"}
    return notify({"channel": "email", "template": "payment_event", "recipient": sender,
                   "vars": {"paymentId": payment_id, "eventType": event, "amount": payment["amount"], "currency": payment["currency"]}}, sender)


class Server(ThreadingHTTPServer):
    # A burst of connections (a load test, a rate-limit probe) must queue, not be reset.
    request_queue_size = 256
    daemon_threads = True


class Handler(BaseHTTPRequestHandler):
    server_version = "notify-api"

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
        if url.path == "/health":
            status, _ = http("GET", BROKER_URL + "/health", timeout=3)
            return self.reply(200 if status == 200 else 503, {"status": "ok" if status == 200 else "the identity broker is not answering"})
        if not who(self.headers):
            return self.reply(401, {"error": "sign in with the identity broker"})
        channel = (parse_qs(url.query).get("channel") or [""])[0]
        with lock:
            if url.path == "/delivered":
                return self.reply(200, {"delivered": [r for r in delivered if not channel or r["channel"] == channel]})
            if url.path == "/dlq":
                return self.reply(200, {"dlq": list(dead)})
            if url.path.startswith("/api/v1/notifications/"):
                wanted = url.path.rsplit("/", 1)[1]
                for record in delivered:
                    if record["id"] == wanted:
                        return self.reply(200, dict(record, status="sent"))
                for record in dead:
                    if record["id"] == wanted:
                        return self.reply(200, dict(record, status="failed"))
        self.reply(404, {"error": "not found"})

    def do_POST(self):
        sender = who(self.headers)
        if not sender:
            return self.reply(401, {"error": "sign in with the identity broker"})
        path = urlparse(self.path).path
        if path == "/api/v1/notifications":
            return self.reply(*notify(self.read_json(), sender))
        if path == "/payment-events":
            return self.reply(*payment_event(self.read_json(), sender))
        self.reply(404, {"error": "not found"})

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    print("notify-api on %d: channels %s, retry %s, DLQ %s, rate limit %s/s" % (port, CHANNELS, RETRY_POLICY, ENABLE_DLQ, RATE_LIMIT), flush=True)
    Server(("0.0.0.0", port), Handler).serve_forever()

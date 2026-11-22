"""A small API that does what the security checks look for.

It is the service under test for security-suite. Every check passes against it
because the service is careful, not because a gateway hides it:

  - security headers on every response, including errors;
  - 404 for paths that must not be public, 405 for dangerous methods;
  - no GraphQL endpoint;
  - CORS only for one trusted origin, and nothing reflected;
  - a rate limit of 20 requests a second on /api/v1/resource.

Break any of these and the matching security step fails, and so does
verify-findings. That is the point of the suite.
"""
import json
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

TRUSTED_ORIGIN = "https://app.example.test"
RATE_LIMIT = 20  # requests per second on /api/v1/resource

lock = threading.Lock()
recent = []


def allowed():
    now = time.time()
    with lock:
        recent[:] = [t for t in recent if now - t < 1.0]
        if len(recent) >= RATE_LIMIT:
            return False
        recent.append(now)
        return True


class Handler(BaseHTTPRequestHandler):
    server_version = "api"
    sys_version = ""

    def end_headers(self):
        # Every response carries the headers, errors included.
        self.send_header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("X-Frame-Options", "DENY")
        self.send_header("Content-Security-Policy", "default-src 'none'")
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header("Permissions-Policy", "geolocation=(), camera=()")
        if self.headers.get("Origin") == TRUSTED_ORIGIN:
            self.send_header("Access-Control-Allow-Origin", TRUSTED_ORIGIN)
        super().end_headers()

    def reply(self, status, body=None):
        payload = b"" if body is None else json.dumps(body).encode()
        self.send_response(status)
        if payload:
            self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        if payload and self.command != "HEAD":
            self.wfile.write(payload)

    def do_GET(self):
        path = urlparse(self.path).path
        if path in ("/", "/api", "/api/v1"):
            return self.reply(200, {"ok": True})
        if path == "/api/v1/resource":
            if not allowed():
                return self.reply(429, {"error": "slow down"})
            return self.reply(200, {"ok": True})
        return self.reply(404, {"error": "not found"})

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)
        if urlparse(self.path).path == "/":
            return self.reply(200, {"ok": True})
        return self.reply(404, {"error": "not found"})

    def method_not_allowed(self):
        return self.reply(405, {"error": "method not allowed"})

    do_PUT = do_DELETE = do_PATCH = do_TRACE = do_TRACK = method_not_allowed

    def log_message(self, *args):
        pass


print("api listening on :8000", flush=True)
ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()

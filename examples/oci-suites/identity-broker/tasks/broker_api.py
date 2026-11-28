"""The identity broker identity-broker tests.

It signs a user in through an identity provider (a mock, by its name), keeps the
session in Redis, and answers who a session belongs to.

  POST /login?provider=oidc|saml|ldap   {email, realm?, ttlSeconds?}  -> {token, ...}
  GET  /session                         Authorization: Bearer <token>
  POST /logout                          revokes the session
  GET  /claims?realm=realm-N            only the realm the session belongs to
  POST /mfa/enroll | /mfa/verify        TOTP, when ENABLE_MFA is true
  GET  /profile                         needs a verified second factor when MFA is on
  GET  /health                          200 only when Redis and the upstream IdP answer

The rule the suite exists to protect: a token is trusted only if its signature
verifies (see verified()). Take that check out and login-smoke, which hands the
broker a tampered token, fails.

Realms are realm-0 .. realm-<REALM_COUNT-1>; a session belongs to one of them and
cannot read another's claims. Sessions live in the session store the pre-hook
starts (REDIS_HOST, "session-store").
"""
import base64
import hashlib
import hmac
import json
import os
import secrets
import socket
import struct
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

PROVIDERS = [p for p in os.environ.get("PROVIDERS", "oidc,saml,ldap").split(",") if p]
REALM_COUNT = int(os.environ.get("REALM_COUNT", "3"))
ENABLE_MFA = os.environ.get("ENABLE_MFA", "true") == "true"
SESSION_TTL = int(os.environ.get("SESSION_TTL_SECONDS", "3600"))
SIGNING_KEY = os.environ.get("JWT_SIGNING_KEY", "dev-signing-key").encode()
REDIS_HOST = os.environ.get("REDIS_HOST", "session-store")
IDP_DISCOVERY = os.environ.get("IDP_DISCOVERY", "http://upstream-idp:9000/.well-known/openid-configuration")


# ── Redis, over its text protocol ───────────────────────────────────────────
def redis(*parts):
    message = b"*%d\r\n" % len(parts) + b"".join(b"$%d\r\n%s\r\n" % (len(str(p).encode()), str(p).encode()) for p in parts)
    with socket.create_connection((REDIS_HOST, 6379), timeout=5) as conn:
        conn.sendall(message)
        reader = conn.makefile("rb")
        line = reader.readline().rstrip(b"\r\n")
        if line.startswith(b"$"):
            size = int(line[1:])
            return None if size < 0 else reader.read(size).decode()
        if line.startswith(b"-"):
            raise RuntimeError(line.decode())
        return line[1:].decode()


# ── tokens ──────────────────────────────────────────────────────────────────
def b64(data):
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def unb64(text):
    return base64.urlsafe_b64decode(text + "=" * (-len(text) % 4))


def sign(payload):
    body = b64(json.dumps(payload, sort_keys=True).encode())
    return body + "." + b64(hmac.new(SIGNING_KEY, body.encode(), hashlib.sha256).digest())


def verified(token):
    """The payload of a token whose signature verifies, or None."""
    try:
        body, signature = token.split(".")
        expected = b64(hmac.new(SIGNING_KEY, body.encode(), hashlib.sha256).digest())
        if not hmac.compare_digest(signature, expected):
            return None
        return json.loads(unb64(body))
    except (ValueError, TypeError):
        return None


def totp(secret_hex, at=None):
    counter = int((at if at is not None else time.time()) // 30)
    digest = hmac.new(bytes.fromhex(secret_hex), struct.pack(">Q", counter), hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    return "%06d" % ((struct.unpack(">I", digest[offset:offset + 4])[0] & 0x7FFFFFFF) % 1000000)


# ── the upstream pieces ─────────────────────────────────────────────────────
def idp_issuer():
    with urllib.request.urlopen(IDP_DISCOVERY, timeout=5) as reply:
        return json.loads(reply.read())["issuer"]


def ask_provider(provider, email):
    request = urllib.request.Request(
        "http://%s-mock:9080/sessions/login?provider=%s" % (provider, provider),
        data=json.dumps({"email": email}).encode(), headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(request, timeout=10) as reply:
        return json.loads(reply.read())


# ── handlers ────────────────────────────────────────────────────────────────
def login(provider, body):
    if provider not in PROVIDERS:
        return 400, {"error": "unknown provider", "providers": PROVIDERS}
    if not isinstance(body, dict) or "@" not in str(body.get("email", "")):
        return 400, {"error": "email is required"}
    realm = body.get("realm", "realm-0")
    if realm not in ["realm-%d" % i for i in range(REALM_COUNT)]:
        return 400, {"error": "unknown realm"}
    ttl = body.get("ttlSeconds", SESSION_TTL)
    if not isinstance(ttl, int) or ttl < 1 or ttl > SESSION_TTL:
        return 400, {"error": "ttlSeconds must be between 1 and %d" % SESSION_TTL}
    try:
        issuer = idp_issuer()  # no discovery document, no tokens
    except (urllib.error.URLError, OSError, ValueError, KeyError):
        return 503, {"error": "the upstream identity provider is unreachable"}
    try:
        granted = ask_provider(provider, body["email"])
    except (urllib.error.URLError, OSError, ValueError):
        return 502, {"error": "the %s provider did not answer" % provider}
    session = {"sid": secrets.token_hex(8), "email": body["email"], "realm": realm, "provider": provider,
               "idpSession": granted.get("sessionId", ""), "iss": issuer, "mfa": not ENABLE_MFA}
    redis("SET", "session:" + session["sid"], json.dumps(session), "EX", ttl)
    return 200, {"token": sign({"sid": session["sid"]}), "provider": provider, "realm": realm,
                 "idpSession": session["idpSession"], "mfaRequired": ENABLE_MFA, "expiresIn": ttl}


def current(headers):
    """The session a request's bearer token names, or an error answer."""
    token = (headers.get("Authorization") or "").removeprefix("Bearer ").strip()
    payload = verified(token)
    if payload is None:
        return None, (401, {"error": "invalid token"})
    stored = redis("GET", "session:" + payload.get("sid", ""))
    if stored is None:
        return None, (401, {"error": "session expired or revoked"})
    return json.loads(stored), None


class Server(ThreadingHTTPServer):
    # A burst of connections (a load test, a rate-limit probe) must queue, not be reset.
    request_queue_size = 256
    daemon_threads = True


class Handler(BaseHTTPRequestHandler):
    server_version = "broker-api"

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
        if url.path == "/health":
            try:
                redis("PING")
                idp_issuer()
            except Exception as err:
                return self.reply(503, {"status": "down", "error": str(err)})
            return self.reply(200, {"status": "ok"})
        session, failure = current(self.headers)
        if failure:
            return self.reply(*failure)
        if url.path == "/session":
            return self.reply(200, {"email": session["email"], "realm": session["realm"], "provider": session["provider"],
                                    "idpSession": session["idpSession"], "mfa": session["mfa"]})
        if url.path == "/claims":
            realm = (query.get("realm") or [session["realm"]])[0]
            if realm != session["realm"]:
                return self.reply(403, {"error": "a session cannot read another realm"})
            return self.reply(200, {"realm": realm, "tier": "enterprise" if realm == "realm-0" else "standard", "email": session["email"]})
        if url.path == "/profile":
            if ENABLE_MFA and not session["mfa"]:
                print("MFA_BYPASS_ATTEMPTED /profile without a verified second factor, session %s" % session["sid"], flush=True)
                return self.reply(403, {"error": "a second factor is required"})
            return self.reply(200, {"email": session["email"], "profile": "ok"})
        self.reply(404, {"error": "not found"})

    def do_POST(self):
        url = urlparse(self.path)
        if url.path == "/login":
            return self.reply(*login((parse_qs(url.query).get("provider") or [""])[0], self.read_json()))
        session, failure = current(self.headers)
        if failure:
            return self.reply(*failure)
        if url.path == "/logout":
            redis("DEL", "session:" + session["sid"])
            return self.reply(200, {"status": "signed out"})
        if url.path == "/mfa/enroll":
            secret = redis("GET", "mfa:" + session["email"])
            if secret is None:
                secret = secrets.token_hex(10)
                redis("SET", "mfa:" + session["email"], secret)
            return self.reply(200, {"secret": secret})
        if url.path == "/mfa/verify":
            secret, code = redis("GET", "mfa:" + session["email"]), str((self.read_json() or {}).get("code", ""))
            if secret is None or code not in (totp(secret, time.time() + d) for d in (-30, 0, 30)):
                return self.reply(401, {"error": "wrong code"})
            if redis("SET", "mfareplay:%s:%s" % (session["email"], code), "1", "NX", "EX", 120) is None:
                print("TOKEN_REPLAY_DETECTED code reused by %s" % session["email"], flush=True)
                return self.reply(409, {"error": "code already used"})
            session["mfa"] = True
            remaining = int(redis("TTL", "session:" + session["sid"]) or 1)
            redis("SET", "session:" + session["sid"], json.dumps(session), "EX", max(remaining, 1))
            return self.reply(200, {"mfa": True})
        self.reply(404, {"error": "not found"})

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    print("broker-api on %d: providers %s, %d realms, MFA %s" % (port, PROVIDERS, REALM_COUNT, ENABLE_MFA), flush=True)
    Server(("0.0.0.0", port), Handler).serve_forever()

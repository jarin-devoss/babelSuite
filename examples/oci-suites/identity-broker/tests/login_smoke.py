import json
import os
import sys
import urllib.error
import urllib.request
from xml.sax.saxutils import escape

API = os.environ.get("API_URL", "http://broker-api:8080")
cases = []


def call(method, path, body=None, headers=None, base=None, raw=False):
    data = body if raw else (None if body is None else json.dumps(body).encode())
    sent = {"Content-Type": "application/json"}
    sent.update(headers or {})
    request = urllib.request.Request((base or API) + path, data=data, method=method, headers=sent)
    try:
        with urllib.request.urlopen(request, timeout=15) as reply:
            text = reply.read()
            return reply.status, (text.decode("utf-8", "replace") if raw else json.loads(text or b"{}"))
    except urllib.error.HTTPError as err:
        text = err.read()
        try:
            return err.code, (text.decode("utf-8", "replace") if raw else json.loads(text or b"{}"))
        except ValueError:
            return err.code, text.decode("utf-8", "replace")


def check(name, fn):
    """Runs one check. A failed assertion is a failed test case, never a skipped one."""
    try:
        fn()
        cases.append((name, None))
    except AssertionError as err:
        cases.append((name, str(err) or "assertion failed"))
    except Exception as err:  # the service not answering is a failure too
        cases.append((name, "%s: %s" % (type(err).__name__, err)))
    print(("ok   " if cases[-1][1] is None else "FAIL ") + name + ("" if cases[-1][1] is None else " - " + cases[-1][1]), flush=True)


def finish(path, suite):
    """Writes the JUnit report for the checks that ran, and exits non-zero if one failed."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    failed = [case for case in cases if case[1]]
    lines = ['<?xml version="1.0" encoding="UTF-8"?>',
             '<testsuite name="%s" tests="%d" failures="%d" errors="0" skipped="0">' % (escape(suite), len(cases), len(failed))]
    for name, error in cases:
        if error:
            lines.append('<testcase classname="%s" name="%s"><failure message="%s"/></testcase>' % (escape(suite), escape(name), escape(error, {'"': "&quot;"})))
        else:
            lines.append('<testcase classname="%s" name="%s"/>' % (escape(suite), escape(name)))
    lines.append("</testsuite>")
    with open(path, "w", encoding="utf-8") as handle:
        handle.write("\n".join(lines))
    sys.exit(1 if failed else 0)


import hashlib
import hmac
import struct
import time


def totp(secret_hex, at=None):
    counter = int((at if at is not None else time.time()) // 30)
    digest = hmac.new(bytes.fromhex(secret_hex), struct.pack(">Q", counter), hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    return "%06d" % ((struct.unpack(">I", digest[offset:offset + 4])[0] & 0x7FFFFFFF) % 1000000)


def sign_in(provider, email, realm="realm-0", **more):
    body = {"email": email, "realm": realm}
    body.update(more)
    status, answer = call("POST", "/login?provider=" + provider, body)
    assert status == 200, "%s login for %s gave %s %s" % (provider, email, status, answer)
    return answer["token"], answer


def bearer(token):
    return {"Authorization": "Bearer " + token}

PROVIDER = os.environ.get("AUTH_PROVIDER", "oidc")
state = {}


def login_through_the_provider():
    token, answer = sign_in(PROVIDER, "admin@babelsuite.test")
    state["token"] = token
    assert answer["provider"] == PROVIDER, "the session is for %s, not %s" % (answer["provider"], PROVIDER)
    assert answer["idpSession"].startswith("sess_"), "the session did not come from the provider: %r" % answer["idpSession"]


def session_belongs_to_the_user():
    status, body = call("GET", "/session", headers=bearer(state["token"]))
    assert status == 200 and body["email"] == "admin@babelsuite.test" and body["provider"] == PROVIDER, "GET /session gave %s %s" % (status, body)


def tampered_token_is_refused():
    body, _, signature = state["token"].partition(".")
    for forged in (body + "." + signature[:-2] + ("AA" if not signature.endswith("AA") else "BB"), body + ".", "garbage", ""):
        status, answer = call("GET", "/session", headers=bearer(forged))
        assert status == 401, "a forged token %r gave %s %s, want 401" % (forged[:24], status, answer)


def unknown_provider_is_refused():
    status, body = call("POST", "/login?provider=kerberos", {"email": "a@b.test"})
    assert status == 400, "an unknown provider gave %s %s, want 400" % (status, body)


def logout_ends_the_session():
    status, _ = call("POST", "/logout", headers=bearer(state["token"]))
    assert status == 200, "logout gave %s" % status
    status, body = call("GET", "/session", headers=bearer(state["token"]))
    assert status == 401, "a signed-out session still answered: %s %s" % (status, body)


check(PROVIDER + ": login goes through the provider", login_through_the_provider)
check(PROVIDER + ": the session belongs to the user who signed in", session_belongs_to_the_user)
check(PROVIDER + ": a forged or empty token is refused", tampered_token_is_refused)
check(PROVIDER + ": an unknown provider is refused", unknown_provider_is_refused)
check(PROVIDER + ": logout ends the session", logout_ends_the_session)
finish("reports/%s-junit.xml" % PROVIDER, "login-smoke-" + PROVIDER)

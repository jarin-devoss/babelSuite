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

REALM = os.environ.get("REALM_ID", "realm-0")
REALMS = ["realm-%d" % i for i in range(int(os.environ.get("REALM_COUNT", "3")))]
state = {}


def own_claims_are_readable():
    state["token"], _ = sign_in("oidc", "iso-%s@babelsuite.test" % REALM, realm=REALM)
    status, body = call("GET", "/claims?realm=" + REALM, headers=bearer(state["token"]))
    assert status == 200 and body["realm"] == REALM, "own claims gave %s %s" % (status, body)


def other_realms_are_denied():
    for other in (r for r in REALMS if r != REALM):
        status, body = call("GET", "/claims?realm=" + other, headers=bearer(state["token"]))
        assert status == 403, "%s read %s: %s %s, want 403" % (REALM, other, status, body)


def a_realm_you_do_not_belong_to_cannot_be_signed_in():
    status, body = call("POST", "/login?provider=oidc", {"email": "x@babelsuite.test", "realm": "realm-999"})
    assert status == 400, "an unknown realm gave %s %s, want 400" % (status, body)


check(REALM + ": own claims are readable", own_claims_are_readable)
check(REALM + ": other realms' claims are denied", other_realms_are_denied)
check(REALM + ": an unknown realm cannot be signed in to", a_realm_you_do_not_belong_to_cannot_be_signed_in)
finish("reports/%s-isolation.xml" % REALM, "realm-isolation-" + REALM)

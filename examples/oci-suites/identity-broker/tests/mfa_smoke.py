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

state = {}


def sensitive_data_needs_the_second_factor():
    state["token"], answer = sign_in("oidc", "mfa@babelsuite.test")
    assert answer["mfaRequired"] is True, "MFA is on, the login did not say a factor is required"
    status, body = call("GET", "/profile", headers=bearer(state["token"]))
    if status == 200:
        print("MFA_BYPASS_ATTEMPTED /profile answered without a second factor")
    assert status == 403, "/profile before the second factor gave %s %s, want 403" % (status, body)


def a_wrong_code_is_refused():
    status, body = call("POST", "/mfa/enroll", headers=bearer(state["token"]))
    assert status == 200 and body.get("secret"), "enrolment gave %s %s" % (status, body)
    state["secret"] = body["secret"]
    status, body = call("POST", "/mfa/verify", {"code": "000000"}, headers=bearer(state["token"]))
    assert status == 401, "a wrong code gave %s %s, want 401" % (status, body)


def the_right_code_unlocks_the_profile():
    code = totp(state["secret"])
    state["code"] = code
    status, body = call("POST", "/mfa/verify", {"code": code}, headers=bearer(state["token"]))
    assert status == 200, "the right code gave %s %s" % (status, body)
    status, body = call("GET", "/profile", headers=bearer(state["token"]))
    assert status == 200, "/profile after the second factor gave %s %s" % (status, body)


def a_used_code_cannot_be_replayed():
    other, _ = sign_in("oidc", "mfa@babelsuite.test")
    status, body = call("POST", "/mfa/verify", {"code": state["code"]}, headers=bearer(other))
    if status == 200:
        print("TOKEN_REPLAY_DETECTED a used code was accepted again")
    assert status == 409, "a replayed code gave %s %s, want 409" % (status, body)


check("sensitive data is refused before the second factor", sensitive_data_needs_the_second_factor)
check("a wrong second-factor code is refused", a_wrong_code_is_refused)
check("the right code unlocks the sensitive data", the_right_code_unlocks_the_profile)
check("a code that was used cannot be used again", a_used_code_cannot_be_replayed)
finish("reports/mfa-junit.xml", "mfa-smoke")

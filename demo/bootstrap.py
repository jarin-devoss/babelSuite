#!/usr/bin/env python3
"""Fill a fresh BabelSuite install with the example content.

Three steps, each safe to repeat:

  1. wait for the OCI registry to answer
  2. push every examples/oci-suites and examples/oci-modules package into it
  3. install every examples/oci-plugins package into the platform settings file

It touches no database and needs no running backend or session, so it can run
before the control plane has ever started. Run it again after editing anything
under examples/ — the backend reads the catalog at startup, so restart it
afterwards.
"""

import argparse
import pathlib
import subprocess
import sys
import time
import urllib.error
import urllib.request

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
SCRIPTS = REPO_ROOT / "scripts"


def wait_for_registry(url: str, timeout: float) -> bool:
    """Poll the OCI v2 endpoint until it answers.

    A 401 counts as ready: an authenticated registry is up, it just wants
    credentials, and that is a different problem from not being up.
    """
    deadline = time.time() + timeout
    probe = url.rstrip("/") + "/v2/"
    last = ""
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(probe, timeout=5) as resp:
                print(f"registry ready at {url} (HTTP {resp.status})")
                return True
        except urllib.error.HTTPError as exc:
            if exc.code in (401, 403):
                print(f"registry ready at {url} (HTTP {exc.code} — needs credentials)")
                return True
            last = f"HTTP {exc.code}"
        except Exception as exc:  # connection refused, DNS, timeout
            last = type(exc).__name__
        time.sleep(2)
    print(f"registry at {url} did not answer within {timeout:.0f}s (last: {last})", file=sys.stderr)
    return False


def run(label: str, argv: list[str]) -> bool:
    print(f"\n=== {label} ===", flush=True)
    result = subprocess.run([sys.executable, *argv], cwd=REPO_ROOT)
    if result.returncode != 0:
        print(f"! {label} failed (exit {result.returncode})", file=sys.stderr)
        return False
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--registry",
        default="http://localhost:5000",
        help="OCI registry to push into (default: http://localhost:5000)",
    )
    parser.add_argument(
        "--settings",
        default="configuration.yaml",
        help="platform settings file to install plugins into, relative to the repo root",
    )
    parser.add_argument("--wait", type=float, default=90.0, help="seconds to wait for the registry")
    parser.add_argument("--skip-suites", action="store_true", help="do not push suites or modules")
    parser.add_argument("--skip-plugins", action="store_true", help="do not install plugins")
    args = parser.parse_args()

    print(f"repo:     {REPO_ROOT}")
    print(f"registry: {args.registry}")
    print(f"settings: {args.settings}")

    if not args.skip_suites:
        if not wait_for_registry(args.registry, args.wait):
            return 1
        if not run("push suites and modules", [str(SCRIPTS / "push-to-zot.py"), "--registry", args.registry]):
            return 1

    if not args.skip_plugins:
        if not run("install plugins", [str(SCRIPTS / "install-plugins.py"), "--settings", args.settings]):
            return 1

    print(
        "\nDone. Next:\n"
        "  cd backend  && go run ./cmd/server\n"
        "  cd frontend && npm install && npm run dev\n"
        "\nThe backend reads the catalog at startup, so start it after this — and if it was\n"
        "already running, restart it. Removing any babel-sidecar-* container makes the\n"
        "APISIX sidecar reload changed plugin Lua on the next run."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())

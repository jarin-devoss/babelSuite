#!/usr/bin/env python3
"""Push the bundled example suites to a local Zot registry as OCI artifacts.

Each examples/oci-suites/<name>/metadata.yaml declares spec.repository and
spec.tags. The suite directory is packed into a single gzipped tar layer, which
is what catalogSuiteReader.pullOCIFiles expects to find as the manifest's first
layer.
"""

import argparse
import gzip
import hashlib
import io
import json
import pathlib
import sys
import tarfile
import urllib.error
import urllib.request

import yaml

CONFIG_MEDIA_TYPE = "application/vnd.oci.image.config.v1+json"
LAYER_MEDIA_TYPE = "application/vnd.oci.image.layer.v1.tar+gzip"
MANIFEST_MEDIA_TYPE = "application/vnd.oci.image.manifest.v1+json"

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
SUITES_DIR = REPO_ROOT / "examples" / "oci-suites"
MODULES_DIR = REPO_ROOT / "examples" / "oci-modules"

# Packed into the layer; everything else in a suite dir is build/editor noise.
SKIP_DIRS = {".git", "__pycache__", "node_modules", ".venv"}


def digest_of(data: bytes) -> str:
    return "sha256:" + hashlib.sha256(data).hexdigest()


def request(method: str, url: str, *, data=None, headers=None):
    req = urllib.request.Request(url, data=data, method=method)
    for key, value in (headers or {}).items():
        req.add_header(key, value)
    return urllib.request.urlopen(req)


def pack_suite(suite_dir: pathlib.Path) -> bytes:
    """Gzipped tar of the suite directory, paths relative to the suite root."""
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode="w") as tar:
        for path in sorted(suite_dir.rglob("*")):
            if not path.is_file():
                continue
            if any(part in SKIP_DIRS for part in path.relative_to(suite_dir).parts):
                continue
            tar.add(path, arcname=str(path.relative_to(suite_dir)).replace("\\", "/"))
    # mtime=0 keeps the digest stable across runs so repeated pushes are no-ops.
    return gzip.compress(raw.getvalue(), mtime=0)


def blob_exists(registry: str, repository: str, digest: str) -> bool:
    try:
        request("HEAD", f"{registry}/v2/{repository}/blobs/{digest}")
        return True
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            return False
        raise


def upload_blob(registry: str, repository: str, payload: bytes) -> str:
    digest = digest_of(payload)
    if blob_exists(registry, repository, digest):
        return digest

    with request("POST", f"{registry}/v2/{repository}/blobs/uploads/") as resp:
        location = resp.headers["Location"]
    if location.startswith("/"):
        location = registry + location
    separator = "&" if "?" in location else "?"

    request(
        "PUT",
        f"{location}{separator}digest={digest}",
        data=payload,
        headers={"Content-Type": "application/octet-stream"},
    )
    return digest


def push(registry: str, repository: str, tags, layer: bytes) -> None:
    config = json.dumps({"architecture": "amd64", "os": "linux"}).encode()
    config_digest = upload_blob(registry, repository, config)
    layer_digest = upload_blob(registry, repository, layer)

    manifest = json.dumps(
        {
            "schemaVersion": 2,
            "mediaType": MANIFEST_MEDIA_TYPE,
            "config": {
                "mediaType": CONFIG_MEDIA_TYPE,
                "digest": config_digest,
                "size": len(config),
            },
            "layers": [
                {
                    "mediaType": LAYER_MEDIA_TYPE,
                    "digest": layer_digest,
                    "size": len(layer),
                }
            ],
        }
    ).encode()

    for tag in tags:
        request(
            "PUT",
            f"{registry}/v2/{repository}/manifests/{tag}",
            data=manifest,
            headers={"Content-Type": MANIFEST_MEDIA_TYPE},
        )
        print(f"  pushed {repository}:{tag}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--registry", default="http://localhost:5000")
    parser.add_argument("--suite", action="append", help="only push these suite ids")
    args = parser.parse_args()

    registry = args.registry.rstrip("/")

    if not SUITES_DIR.is_dir():
        print(f"no example suites at {SUITES_DIR}", file=sys.stderr)
        return 1

    pushed = 0
    manifests = sorted(SUITES_DIR.glob("*/metadata.yaml")) + sorted(MODULES_DIR.glob("*/module.yaml"))
    for metadata_path in manifests:
        suite_dir = metadata_path.parent
        if args.suite and suite_dir.name not in args.suite:
            continue

        spec = (yaml.safe_load(metadata_path.read_text(encoding="utf-8")) or {}).get("spec", {})
        repository = str(spec.get("repository", "")).strip()
        tags = [str(t).strip() for t in (spec.get("tags") or []) if str(t).strip()]
        if not repository or not tags:
            print(f"! {suite_dir.name}: metadata.yaml has no repository/tags, skipped")
            continue

        # spec.repository is registry-qualified (localhost:5000/team/name); the
        # /v2/ API wants only the path after the host.
        _, _, repo_path = repository.partition("/")
        if not repo_path:
            print(f"! {suite_dir.name}: repository {repository!r} has no path, skipped")
            continue

        print(f"{suite_dir.name} -> {repo_path}")
        try:
            push(registry, repo_path, tags, pack_suite(suite_dir))
            pushed += 1
        except urllib.error.HTTPError as exc:
            print(f"  failed: {exc.code} {exc.reason} {exc.read()[:200]!r}", file=sys.stderr)
        except urllib.error.URLError as exc:
            print(f"  failed: {exc.reason}", file=sys.stderr)
            return 1

    print(f"\npushed {pushed} suite(s) to {registry}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

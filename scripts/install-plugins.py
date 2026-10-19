#!/usr/bin/env python3
"""Install the example Lua plugins into the platform settings file.

Each examples/oci-plugins/<name>/plugin.yaml names its Lua, CUE schema and
Starlark wrapper by filename. This reads those files and writes the contents
inline into the `plugins:` list of configuration.yaml, which is where the
control plane looks for registered plugins.

It writes the file rather than calling the API on purpose: platform settings are
read from disk at request time, so this works before the backend has ever
started and needs no session, no CSRF token and no running server. Re-running it
replaces the entries it owns and leaves everything else in the file alone.
"""

import argparse
import pathlib
import sys

import yaml

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
PLUGINS_DIR = REPO_ROOT / "examples" / "oci-plugins"

# Mirrors PluginTriggerRoute in internal/platform/defaults.go. The route is
# derived from the name rather than declared, so a plugin package never states
# it and the two cannot drift — but writing the file directly bypasses the API
# that would normally derive it, so it has to be written here too.
TRIGGER_PREFIX = "/_babelsuite/plugins/"
TRIGGER_SUFFIX = "/start"


def trigger_route(name: str) -> str:
    return TRIGGER_PREFIX + name + TRIGGER_SUFFIX


def read_manifest(manifest_path: pathlib.Path) -> dict:
    """Turn one plugin.yaml into the entry configuration.yaml expects."""
    spec = yaml.safe_load(manifest_path.read_text(encoding="utf-8")) or {}
    suite_dir = manifest_path.parent

    name = str(spec.get("name", "")).strip()
    if not name:
        raise ValueError(f"{manifest_path}: no name")
    if name != suite_dir.name:
        # The Lua compares ngx.var.uri against its own _M.name, so a manifest
        # name that disagrees with the directory would register a route the
        # plugin refuses to answer.
        raise ValueError(f"{manifest_path}: name {name!r} does not match directory {suite_dir.name!r}")

    entry = {
        "name": name,
        "trigger": trigger_route(name),
        "kind": str(spec.get("kind", "plugin")).strip() or "plugin",
        "variants": list(spec.get("variants") or [name]),
    }
    if spec.get("operations"):
        entry["operations"] = list(spec["operations"])

    # lua, schema and star are filenames relative to the plugin folder.
    for field in ("lua", "schema", "star"):
        ref = spec.get(field)
        if not ref:
            continue
        target = suite_dir / str(ref)
        if not target.is_file():
            raise FileNotFoundError(f"{manifest_path}: {field} -> {ref} does not exist")
        entry[field] = target.read_text(encoding="utf-8")

    if spec.get("version"):
        entry["version"] = str(spec["version"])
    if spec.get("deprecated"):
        entry["deprecated"] = True
    return entry


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--settings",
        default=str(REPO_ROOT / "configuration.yaml"),
        help="platform settings file to update (default: ./configuration.yaml)",
    )
    parser.add_argument("--plugin", action="append", help="only install these plugin names")
    parser.add_argument(
        "--prune",
        action="store_true",
        help="also remove registered plugins that no longer exist under examples/oci-plugins",
    )
    parser.add_argument("--dry-run", action="store_true", help="report what would change and exit")
    args = parser.parse_args()

    if not PLUGINS_DIR.is_dir():
        print(f"no example plugins at {PLUGINS_DIR}", file=sys.stderr)
        return 1

    manifests = sorted(PLUGINS_DIR.glob("*/plugin.yaml"))
    if args.plugin:
        wanted = set(args.plugin)
        manifests = [m for m in manifests if m.parent.name in wanted]
        missing = wanted - {m.parent.name for m in manifests}
        if missing:
            print(f"no such plugin(s): {', '.join(sorted(missing))}", file=sys.stderr)
            return 1

    entries = []
    for manifest in manifests:
        try:
            entries.append(read_manifest(manifest))
        except (ValueError, FileNotFoundError) as exc:
            print(f"! {exc}", file=sys.stderr)
            return 1

    settings_path = pathlib.Path(args.settings)
    template = REPO_ROOT / "demo" / "configuration.demo.yaml"
    if not settings_path.is_file() and template.is_file():
        # The settings file is per-machine and not in git, so a fresh checkout
        # has none yet. Start it from the demo settings, as the server does on
        # its first start, rather than from a stub that looks complete.
        settings_path.write_text(template.read_text(encoding="utf-8"), encoding="utf-8", newline="\n")
        print(f"created {settings_path} from {template.relative_to(REPO_ROOT)}")
    if settings_path.is_file():
        settings = yaml.safe_load(settings_path.read_text(encoding="utf-8")) or {}
    else:
        print(f"! {settings_path} does not exist, and neither does {template} to start it from",
              file=sys.stderr)
        return 1

    existing = list(settings.get("plugins") or [])
    by_name = {str(p.get("name")): p for p in existing}

    added, replaced = [], []
    for entry in entries:
        if entry["name"] in by_name:
            replaced.append(entry["name"])
        else:
            added.append(entry["name"])
        by_name[entry["name"]] = entry

    removed = []
    if args.prune:
        known = {m.parent.name for m in sorted(PLUGINS_DIR.glob("*/plugin.yaml"))}
        for name in list(by_name):
            if name not in known:
                del by_name[name]
                removed.append(name)

    # Stable order so the file does not churn between runs.
    settings["plugins"] = [by_name[name] for name in sorted(by_name)]

    for label, names in (("installed", added), ("updated", replaced), ("removed", removed)):
        for name in names:
            print(f"  {label:<10} {name}")

    if args.dry_run:
        print(f"\ndry run — {settings_path} not written")
        return 0

    with settings_path.open("w", encoding="utf-8", newline="\n") as fh:
        yaml.safe_dump(settings, fh, sort_keys=False, default_flow_style=False, allow_unicode=True, width=10**6)

    print(f"\n{len(settings['plugins'])} plugin(s) registered in {settings_path}")
    print("Restart the backend, and remove any babel-sidecar-* container so it reloads the Lua.")
    return 0


if __name__ == "__main__":
    sys.exit(main())

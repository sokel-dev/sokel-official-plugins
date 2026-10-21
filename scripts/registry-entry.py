#!/usr/bin/env python3
"""Rewrites a plugin's catalog entry for a release (run by .github/workflows/release.yml).

    registry-entry.py --plugin discord --version v1.0.1 \
        --image ghcr.io/sokel-dev/sokel-plugin-discord:1.0.1@sha256:... \
        --export export.yml --catalog <checkout of sokel-dev/sokel-registry>

A first release creates its entry from the export, <plugin>/docs and <plugin>/locales.
An existing entry (plugins/sokel/<plugin>/manifest.yml in the catalog) becomes: the contract as `sokel-gen export yaml`
emits it from the plugin's code (credential, operations, events...), the entry's own plugin section (org, label,
desc, doc, icon) with the new version, and its deployment section with the container target pointing at the
released image, pinned by digest. An entry that declares capabilities (`implements`) keeps its shape — which
operations sit under which capability, and which inputs each lists (a capability tier may leave out optional inputs
it does not support) — but the definitions of the inputs and outputs it lists are refreshed from the export, so a
field added inside an existing input (a new filter bound, say) reaches the catalog. Inputs the export has and the
entry does not list are reported, not added: whether a tier takes them is the platform catalog tools' call.

Prints one JSON line: {"previous": ..., "version": ..., "contract_refreshed": bool, "contract_changed": bool,
"unlisted_inputs": [...]}.
Exits 2 when the version does not go up (the catalog's version gate would refuse it anyway).
"""
import argparse
import json
from pathlib import Path
import shutil
import re
import sys

import yaml


def semver(v):
    m = re.fullmatch(r"v?(\d+)\.(\d+)\.(\d+)", v.strip())
    if not m:
        sys.exit(f"not a version: {v!r} (want vX.Y.Z)")
    return tuple(int(x) for x in m.groups())


def by_id(ops):
    return {o.get("id"): o for o in (ops or [])}


def contract_of(doc):
    """The comparable contract: every section but plugin/deployment/implements, operation lists keyed by id."""
    out = {}
    for k, v in doc.items():
        if k in ("plugin", "deployment", "implements"):
            continue
        out[k] = by_id(v) if k in ("operations", "events") and isinstance(v, list) else v
    return out


def refresh_listed(entry, export):
    """Refreshes, in place, the definitions of the inputs and outputs an implements-declaring entry lists — under
    implements and in a flat operations list alike — from the export's operation of the same id. Returns the inputs
    the export has that the entry does not list, as "op.input"."""
    ops = by_id(export.get("operations"))
    unlisted = []

    def fix(op):
        new = ops.get(op.get("id"))
        if not new:
            return
        for key in ("inputs", "outputs"):
            fresh = {f.get("name"): f for f in new.get(key) or []}
            listed = op.get(key) or []
            for i, f in enumerate(listed):
                if f.get("name") in fresh:
                    listed[i] = fresh[f["name"]]
            if key == "inputs":
                names = {f.get("name") for f in listed}
                unlisted.extend(f"{op['id']}.{n}" for n in fresh if n not in names)

    for cap in entry.get("implements") or []:
        for op in cap.get("operations") or []:
            fix(op)
    for op in entry.get("operations") or []:
        fix(op)
    return unlisted


def dump(obj):
    return yaml.safe_dump(obj, sort_keys=False, allow_unicode=True, width=1000, default_flow_style=False)


def display_strings(value):
    """Collect display keys to retire translations whose declarations changed."""
    keys = set()
    if isinstance(value, dict):
        for name, item in value.items():
            if name in ("label", "desc", "help", "placeholder") and isinstance(item, str):
                keys.add(item)
            keys.update(display_strings(item))
    elif isinstance(value, list):
        for item in value:
            keys.update(display_strings(item))
    return keys


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--plugin", required=True)
    ap.add_argument("--version", required=True)
    ap.add_argument("--image", required=True, help="image reference, pinned by digest")
    ap.add_argument("--export", required=True, help="output of sokel-gen export yaml")
    ap.add_argument("--catalog", required=True, help="checkout of the catalog repository")
    a = ap.parse_args()

    path = f"{a.catalog}/plugins/sokel/{a.plugin}/manifest.yml"
    export_text = Path(a.export).read_text(encoding="utf-8")
    export = yaml.safe_load(export_text)
    if export.get("plugin", {}).get("name") != a.plugin:
        sys.exit(f"export is for {export.get('plugin', {}).get('name')!r}, not {a.plugin}")
    first_release = not Path(path).exists()
    if first_release:
        # The first release supplies the contract and local documentation; the built image supplies its digest.
        if not re.fullmatch(r"[a-z0-9][a-z0-9-]*", a.plugin):
            sys.exit("invalid plugin name")
        if not re.search(r"@sha256:[0-9a-f]{64}$", a.image):
            sys.exit("a first official release requires an image digest")
        plugin = dict(export["plugin"], org="sokel", version="v0.0.0")
        source = Path(a.plugin).resolve()
        doc = (source / plugin.get("doc", "docs/" + a.plugin + ".md")).resolve()
        if not doc.is_relative_to(source) or not doc.is_file():
            sys.exit("a first release requires a local usage document")
        locales = source / "locales"
        if not (locales / "en.json").is_file():
            sys.exit("a first release requires locales/en.json")
        plugin["doc"] = "README.md"
        entry = {"plugin": plugin}
        entry_text = ""
    else:
        entry_text = Path(path).read_text(encoding="utf-8")
        entry = yaml.safe_load(entry_text)

    previous = str(entry["plugin"].get("version", "v0.0.0"))
    if semver(a.version) <= semver(previous):
        print(f"{a.plugin}: {a.version} does not go up from {previous}", file=sys.stderr)
        sys.exit(2)
    # deployment: the first container target gets the released image; everything else stays.
    deployment = entry.get("deployment") or {"targets": [{"kind": "container", "ref": a.image}]}
    for t in deployment.get("targets", []):
        if t.get("kind") == "container":
            t["ref"] = a.image
            break
    else:
        deployment.setdefault("targets", []).append({"kind": "container", "ref": a.image})

    plugin = dict(entry["plugin"])
    plugin["name"] = a.plugin
    plugin["version"] = a.version
    old_strings = display_strings(entry)
    target_root = Path(path).parent.resolve()
    target_doc = (target_root / plugin.get("doc", "README.md")).resolve()
    local_doc = target_doc.is_relative_to(target_root) and "://" not in plugin.get("doc", "")
    if local_doc and target_doc.is_file():
        old_strings.add(target_doc.read_text(encoding="utf-8"))

    refreshed = "implements" not in entry
    changed = contract_of(export) != contract_of(entry)
    if refreshed:
        # The export text as sokel-gen wrote it (minus its header comment), with the plugin block replaced and the
        # deployment block appended: the entry's formatting then follows the exporter, release after release.
        body = re.sub(r"\A(?:#.*\n|\n)*", "", export_text)
        body, n = re.subn(r"(?m)^plugin:\n(?:[ \t]+.*\n)*", dump({"plugin": plugin}), body, count=1)
        if n != 1:
            sys.exit("export has no plugin block")
        text = body.rstrip("\n") + "\n" + dump({"deployment": deployment})
        unlisted = []
    else:
        unlisted = refresh_listed(entry, export)
        entry["plugin"] = plugin
        entry["deployment"] = deployment
        head = "".join(l for l in entry_text.splitlines(True) if l.startswith("#"))  # leading comment header
        text = head + dump(entry)
    if first_release:
        target = Path(path).parent
        target.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(doc, target / "README.md")
        shutil.copytree(locales, target / "locales", dirs_exist_ok=True)
    else:
        # New translations, including usage markdown, ship with every release.
        # Keep catalog-only translations for catalog-owned descriptions and notes.
        source_root = Path(a.plugin).resolve()
        source_doc = (source_root / export.get("plugin", {}).get("doc", "docs/" + a.plugin + ".md")).resolve()
        if local_doc and source_doc.is_relative_to(source_root) and source_doc.is_file():
            target_doc.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source_doc, target_doc)
        current_strings = display_strings(yaml.safe_load(text))
        if local_doc and target_doc.is_file():
            current_strings.add(target_doc.read_text(encoding="utf-8"))
        retired = old_strings - current_strings
        target = Path(path).parent / "locales"
        for locale in (Path(a.plugin) / "locales").glob("*.json"):
            target.mkdir(parents=True, exist_ok=True)
            dest = target / locale.name
            merged = json.loads(dest.read_text(encoding="utf-8")) if dest.exists() else {}
            merged.update(json.loads(locale.read_text(encoding="utf-8")))
            for key in retired:
                merged.pop(key, None)
            dest.write_text(json.dumps(merged, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    Path(path).write_text(text, encoding="utf-8")
    print(json.dumps({"previous": previous, "version": a.version, "contract_refreshed": refreshed, "contract_changed": changed,
                      "unlisted_inputs": unlisted}))


def dump_deployment_in_place(text, deployment):
    """Replaces the top-level deployment block of an entry kept otherwise as it is."""
    block = dump({"deployment": deployment})
    text, n = re.subn(r"(?m)^deployment:\n(?:[ \t]+.*\n|\n(?=[ \t]))*", block, text, count=1)
    return text if n == 1 else text.rstrip("\n") + "\n" + block


if __name__ == "__main__":
    main()

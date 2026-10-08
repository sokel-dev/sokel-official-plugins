#!/usr/bin/env python3
"""Rewrites a plugin's catalog entry for a release (run by .github/workflows/release.yml).

    registry-entry.py --plugin discord --version v1.0.1 \
        --image ghcr.io/sokel-dev/sokel-plugin-discord:1.0.1@sha256:... \
        --export export.yml --catalog <checkout of sokel-dev/sokel-registry>

The entry (plugins/sokel/<plugin>/manifest.yml in the catalog) becomes: the contract as `sokel-gen export yaml`
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


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--plugin", required=True)
    ap.add_argument("--version", required=True)
    ap.add_argument("--image", required=True, help="image reference, pinned by digest")
    ap.add_argument("--export", required=True, help="output of sokel-gen export yaml")
    ap.add_argument("--catalog", required=True, help="checkout of the catalog repository")
    a = ap.parse_args()

    path = f"{a.catalog}/plugins/sokel/{a.plugin}/manifest.yml"
    try:
        entry_text = open(path, encoding="utf-8").read()
    except OSError:
        sys.exit(f"no catalog entry at {path}: a first release needs the entry created by hand (see CONTRIBUTING there)")
    entry = yaml.safe_load(entry_text)
    export_text = open(a.export, encoding="utf-8").read()
    export = yaml.safe_load(export_text)

    previous = str(entry["plugin"].get("version", "v0.0.0"))
    if semver(a.version) <= semver(previous):
        print(f"{a.plugin}: {a.version} does not go up from {previous}", file=sys.stderr)
        sys.exit(2)
    if export.get("plugin", {}).get("name") != a.plugin:
        sys.exit(f"export is for {export.get('plugin', {}).get('name')!r}, not {a.plugin}")

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
    open(path, "w", encoding="utf-8").write(text)
    print(json.dumps({"previous": previous, "version": a.version, "contract_refreshed": refreshed, "contract_changed": changed,
                      "unlisted_inputs": unlisted}))


def dump_deployment_in_place(text, deployment):
    """Replaces the top-level deployment block of an entry kept otherwise as it is."""
    block = dump({"deployment": deployment})
    text, n = re.subn(r"(?m)^deployment:\n(?:[ \t]+.*\n|\n(?=[ \t]))*", block, text, count=1)
    return text if n == 1 else text.rstrip("\n") + "\n" + block


if __name__ == "__main__":
    main()

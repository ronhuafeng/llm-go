#!/usr/bin/env python3
"""Report generated compatibility impact from classified manifests."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any

import codexsdk_generate_sdk_surface as sdk_surface


def load_manifest(path: Path) -> dict[str, Any]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"manifest {path} is not an object")
    return value


def surface_index(manifest: dict[str, Any]) -> dict[tuple[str, str], dict[str, str]]:
    result: dict[tuple[str, str], dict[str, str]] = {}
    generated_surface = list(manifest.get("surface", [])) + sdk_surface.facade_compatibility_surface(manifest)
    for raw in generated_surface:
        if not isinstance(raw, dict):
            raise ValueError("manifest surface entry is not an object")
        kind = raw.get("kind")
        name = raw.get("name")
        stability = raw.get("stability")
        signature = raw.get("signature")
        if not all(isinstance(value, str) and value for value in (kind, name, stability, signature)):
            raise ValueError(f"manifest surface entry is unclassified: {raw!r}")
        key = (kind, name)
        if key in result:
            raise ValueError(f"duplicate manifest surface identity: {kind} {name}")
        result[key] = {"classification": stability, "signature": signature}
    if not result:
        raise ValueError("manifest has no classified generated surface")
    return result


def compatibility_report(base: dict[str, Any], target: dict[str, Any]) -> dict[str, Any]:
    before = surface_index(base)
    after = surface_index(target)
    added = []
    removed = []
    reclassified = []
    changed = []
    for kind, name in sorted(after.keys() - before.keys()):
        added.append({"kind": kind, "name": name, **after[(kind, name)]})
    for kind, name in sorted(before.keys() - after.keys()):
        removed.append({"kind": kind, "name": name, **before[(kind, name)]})
    for kind, name in sorted(before.keys() & after.keys()):
        if before[(kind, name)]["classification"] != after[(kind, name)]["classification"]:
            reclassified.append(
                {
                    "kind": kind,
                    "name": name,
                    "from": before[(kind, name)]["classification"],
                    "to": after[(kind, name)]["classification"],
                }
            )
        if before[(kind, name)]["signature"] != after[(kind, name)]["signature"]:
            changed.append(
                {
                    "kind": kind,
                    "name": name,
                    "classification": before[(kind, name)]["classification"],
                    "from_signature": before[(kind, name)]["signature"],
                    "to_signature": after[(kind, name)]["signature"],
                }
            )
    implementation_obligations = [item for item in added if item["kind"] == "interface_method"]
    return {
        "policy": "go_source_compatibility_with_classification_metadata",
        "compatibility_impact": "incompatible" if removed or changed or implementation_obligations else "additive_or_metadata_only",
        "added": added,
        "removed": removed,
        "reclassified": reclassified,
        "changed": changed,
        "external_implementation_obligations": implementation_obligations,
        "counts_by_classification": {
            stability: {
                "added": sum(item["classification"] == stability for item in added),
                "removed": sum(item["classification"] == stability for item in removed),
                "changed": sum(item["classification"] == stability for item in changed),
                "reclassified_from": sum(item["from"] == stability for item in reclassified),
                "reclassified_to": sum(item["to"] == stability for item in reclassified),
            }
            for stability in ("stable", "experimental", "mixed")
        },
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-manifest", required=True, type=Path)
    parser.add_argument("--target-manifest", required=True, type=Path)
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    report = compatibility_report(load_manifest(args.base_manifest), load_manifest(args.target_manifest))
    encoded = json.dumps(report, indent=2, ensure_ascii=False) + "\n"
    if args.out:
        args.out.write_text(encoded, encoding="utf-8")
    else:
        print(encoded, end="")
    return 1 if report["compatibility_impact"] == "incompatible" else 0


if __name__ == "__main__":
    raise SystemExit(main())

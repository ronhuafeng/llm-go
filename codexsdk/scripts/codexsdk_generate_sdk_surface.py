#!/usr/bin/env python3
"""Compatibility-surface facts for sync reports.

Generated `sdk_surface.gen.go` is owned by `generatedproof`. This module must
not write SDK source.
"""

from __future__ import annotations

import json
import re
from pathlib import Path


FACADE_TARGET_RE = re.compile(r"^([A-Za-z][A-Za-z0-9]*)\(\)\.([A-Za-z][A-Za-z0-9]*)$")
FACADE_STATUS_GENERATED = "generated"


def load_json(path: Path) -> object:
    return json.loads(path.read_text(encoding="utf-8"))


def facade_compatibility_surface(manifest: dict[str, object]) -> list[dict[str, str]]:
    by_accessor: dict[str, list[dict[str, str]]] = {}
    for raw_entry in manifest.get("entries", []):
        entry = raw_entry if isinstance(raw_entry, dict) else {}
        if entry.get("direction") != "client_to_server" or entry.get("kind") != "request":
            continue
        match = FACADE_TARGET_RE.match(str(entry.get("facade_target", "")))
        if not match or entry.get("facade_status") != FACADE_STATUS_GENERATED:
            continue
        accessor, operation = match.groups()
        params_type = str(entry.get("params_or_payload_schema", ""))
        response_type = str(entry.get("response_type", ""))
        stability = str(entry.get("stability", ""))
        signature = "func(context.Context"
        if params_type:
            signature += f", protocolv2.{params_type}"
        signature += f") (protocolv2.{response_type}, error)"
        by_accessor.setdefault(accessor, []).append(
            {
                "kind": "method",
                "name": f"codexsdk.{accessor}.{operation}",
                "owner": f"codexsdk.{accessor}",
                "signature": signature,
                "stability": stability,
            }
        )

    surface: list[dict[str, str]] = []
    for accessor, methods in by_accessor.items():
        stabilities = {method["stability"] for method in methods}
        type_stability = "mixed" if len(stabilities) > 1 else next(iter(stabilities))
        surface.append(
            {
                "kind": "type",
                "name": f"codexsdk.{accessor}",
                "owner": "",
                "signature": "struct{/* opaque */}",
                "stability": type_stability,
            }
        )
        surface.extend(methods)
    return sorted(surface, key=lambda entry: (entry["kind"], entry["name"]))

#!/usr/bin/env python3
"""Generate the public SDK facade surface from the checked-in protocol manifest."""

from __future__ import annotations

import argparse
import json
import re
from dataclasses import dataclass
from pathlib import Path


DEFAULT_BASELINE = Path("codexsdk/internal/protocolschema/appserver/v2")
DEFAULT_METHOD_REGISTRY = Path("codexsdk/protocolv2/method_registry.gen.go")
DEFAULT_PROTOCOL_TYPES = Path("codexsdk/protocolv2/protocol_types.gen.go")
DEFAULT_OUTPUT = Path("codexsdk/sdk_surface.gen.go")

FACADE_TARGET_RE = re.compile(r"^([A-Za-z][A-Za-z0-9]*)\(\)\.([A-Za-z][A-Za-z0-9]*)$")
METHOD_CONST_RE = re.compile(r'^\s*(Method[A-Za-z0-9]+)\s+=\s+"([^"]+)"', re.MULTILINE)
TYPE_DECL_RE = re.compile(r"^type\s+([A-Za-z][A-Za-z0-9]*)\b", re.MULTILINE)


@dataclass(frozen=True)
class SurfaceMethod:
    accessor: str
    operation: str
    method: str
    method_const: str
    params_type: str
    response_type: str
    stability: str


FACADE_STATUS_GENERATED = "generated"
FACADE_STATUS_DEFERRED = "deferred_missing_generated_types"


def load_json(path: Path) -> object:
    return json.loads(path.read_text(encoding="utf-8"))


def method_constants(path: Path) -> dict[str, str]:
    text = path.read_text(encoding="utf-8")
    return {method: const for const, method in METHOD_CONST_RE.findall(text)}


def protocol_type_names(path: Path) -> set[str]:
    return set(TYPE_DECL_RE.findall(path.read_text(encoding="utf-8")))


def facade_struct_name(accessor: str) -> str:
    return accessor


def surface_methods(manifest: dict[str, object], method_consts: dict[str, str], type_names: set[str]) -> list[SurfaceMethod]:
    methods: list[SurfaceMethod] = []
    seen: set[tuple[str, str]] = set()
    for raw_entry in manifest.get("entries", []):
        entry = raw_entry if isinstance(raw_entry, dict) else {}
        if entry.get("direction") != "client_to_server" or entry.get("kind") != "request":
            continue
        target = str(entry.get("facade_target", ""))
        match = FACADE_TARGET_RE.match(target)
        if not match:
            if target.startswith("internal."):
                continue
            raise SystemExit(f"invalid generated facade target {target!r} for method {entry.get('method', '')!r}")
        accessor, operation = match.groups()
        method = str(entry.get("method", ""))
        facade_status = str(entry.get("facade_status", ""))
        method_const = method_consts.get(method, "")
        params_type = str(entry.get("params_or_payload_schema", ""))
        response_type = str(entry.get("response_type", ""))
        missing = []
        if not method_const:
            missing.append("method constant")
        if params_type and params_type not in type_names:
            missing.append(f"params type {params_type}")
        if not response_type or response_type not in type_names:
            missing.append(f"response type {response_type or '<empty>'}")
        if facade_status == FACADE_STATUS_DEFERRED:
            if not missing:
                raise SystemExit(f"deferred facade method {method!r} has all generated prerequisites; mark it generated")
            continue
        if facade_status != FACADE_STATUS_GENERATED:
            raise SystemExit(f"facade method {method!r} has invalid or missing facade_status {facade_status!r}")
        if missing:
            raise SystemExit(f"generated facade method {method!r} is missing {', '.join(missing)}")
        key = (accessor, operation)
        if key in seen:
            raise SystemExit(f"duplicate facade operation {accessor}().{operation}")
        seen.add(key)
        methods.append(
            SurfaceMethod(
                accessor=accessor,
                operation=operation,
                method=method,
                method_const=method_const,
                params_type=params_type,
                response_type=response_type,
                stability=str(entry.get("stability", "")),
            )
        )
    methods.sort(key=lambda item: (item.accessor, item.operation, item.method))
    return methods


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


def render(methods: list[SurfaceMethod]) -> str:
    by_accessor: dict[str, list[SurfaceMethod]] = {}
    for method in methods:
        by_accessor.setdefault(method.accessor, []).append(method)
    accessors = sorted(by_accessor)

    lines: list[str] = [
        "// Code generated by scripts/codexsdk_generate_sdk_surface.py; DO NOT EDIT.",
        "",
        "package codexsdk",
        "",
        "import (",
        '\t"context"',
        "",
        '\t"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"',
        ")",
        "",
        "",
    ]

    for accessor in accessors:
        struct_name = facade_struct_name(accessor)
        lines.extend(
            [
                f"// {struct_name} is an opaque generated facade for exact Codex operations.",
                f"type {struct_name} struct {{",
                "\tclient *Client",
                "}",
                "",
            ]
        )
        lines.extend(
            [
                f"func (c *Client) {accessor}() {struct_name} {{",
                f"\treturn {struct_name}{{client: c}}",
                "}",
                "",
            ]
        )

    for method in methods:
        struct_name = facade_struct_name(method.accessor)
        if method.params_type:
            lines.extend(
                [
                    f"func (f {struct_name}) {method.operation}(ctx context.Context, params protocolv2.{method.params_type}) (protocolv2.{method.response_type}, error) {{",
                    f"\tvar response protocolv2.{method.response_type}",
                    f"\tif err := f.client.callProtocol(ctx, protocolv2.{method.method_const}, params, &response); err != nil {{",
                    f"\t\treturn protocolv2.{method.response_type}{{}}, err",
                    "\t}",
                    "\treturn response, nil",
                    "}",
                    "",
                ]
            )
        else:
            lines.extend(
                [
                    f"func (f {struct_name}) {method.operation}(ctx context.Context) (protocolv2.{method.response_type}, error) {{",
                    f"\tvar response protocolv2.{method.response_type}",
                    f"\tif err := f.client.callProtocolNoParams(ctx, protocolv2.{method.method_const}, &response); err != nil {{",
                    f"\t\treturn protocolv2.{method.response_type}{{}}, err",
                    "\t}",
                    "\treturn response, nil",
                    "}",
                    "",
                ]
            )
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", type=Path, default=DEFAULT_BASELINE)
    parser.add_argument("--method-registry", type=Path, default=DEFAULT_METHOD_REGISTRY)
    parser.add_argument("--protocol-types", type=Path, default=DEFAULT_PROTOCOL_TYPES)
    parser.add_argument("--out", type=Path, default=DEFAULT_OUTPUT)
    args = parser.parse_args()

    manifest = load_json(args.baseline / "manifest.json")
    if not isinstance(manifest, dict):
        raise SystemExit("manifest root must be an object")
    generated = render(surface_methods(manifest, method_constants(args.method_registry), protocol_type_names(args.protocol_types)))
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(generated, encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

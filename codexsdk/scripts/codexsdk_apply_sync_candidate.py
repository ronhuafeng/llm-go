#!/usr/bin/env python3
"""Apply a reviewed Codex upstream schema candidate to the checked-in baseline.

This script intentionally handles the large mechanical part of a protocol
baseline sync so GitHub Actions does not need to ask a Codex agent to inspect
or rewrite thousands of lines of schema/report/generated output.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import codexsdk_schema_diff as schema_diff
import codexsdk_schema_utils as schema_utils
import codexsdk_classified_manifest as classified_manifest
import codexsdk_release_report as release_report


DEFAULT_BASELINE = Path("codexsdk/internal/protocolschema/appserver/v2")
COMMON_RS_REF = "codex-rs/app-server-protocol/src/protocol/common.rs"

FAMILY_ACCESSORS = {
    "account": "Accounts()",
    "app": "Apps()",
    "applyPatchApproval": "ServerRequests()",
    "collaborationMode": "CollaborationModes()",
    "command": "Commands()",
    "config": "Config()",
    "configRequirements": "ConfigRequirements()",
    "configWarning": "ServerNotifications()",
    "deprecationNotice": "ServerNotifications()",
    "error": "ServerNotifications()",
    "execCommandApproval": "ServerRequests()",
    "experimentalFeature": "ExperimentalFeatures()",
    "externalAgentConfig": "ExternalAgentConfigs()",
    "feedback": "Feedback()",
    "fs": "FS()",
    "fuzzyFileSearch": "FuzzyFileSearch()",
    "guardianWarning": "ServerNotifications()",
    "hook": "Hooks()",
    "hooks": "Hooks()",
    "initialize": "Initialize()",
    "initialized": "ClientNotifications()",
    "marketplace": "Marketplace()",
    "mcpServer": "MCPServers()",
    "mcpServerStatus": "MCPServerStatus()",
    "memory": "Memory()",
    "mock": "Mock()",
    "model": "Models()",
    "modelProvider": "ModelProviders()",
    "plugin": "Plugins()",
    "process": "Processes()",
    "remoteControl": "RemoteControl()",
    "review": "Reviews()",
    "serverRequest": "ServerRequests()",
    "skills": "Skills()",
    "thread": "Threads()",
    "turn": "Turns()",
    "warning": "ServerNotifications()",
    "windows": "Windows()",
    "windowsSandbox": "WindowsSandbox()",
    "attestation": "ServerRequests()",
    "environment": "Environments()",
    "permissionProfile": "PermissionProfiles()",
}

ROOT_OPERATION_OVERRIDES = {
    "applyPatchApproval": "ApplyPatchApproval",
    "configWarning": "ConfigWarning",
    "deprecationNotice": "DeprecationNotice",
    "error": "Error",
    "execCommandApproval": "ExecCommandApproval",
    "fuzzyFileSearch": "Search",
    "guardianWarning": "GuardianWarning",
    "initialize": "Initialize",
    "initialized": "Initialized",
    "warning": "Warning",
}

ROOT_INTERNAL_OVERRIDES = {
    "initialize": "internal.InitializeHandshake.Request",
    "initialized": "internal.InitializeHandshake.InitializedNotification",
}


@dataclass(frozen=True)
class RequestMapping:
    variant: str
    response_type: str
    experimental: bool
    macro_name: str


@dataclass(frozen=True)
class AggregateEntry:
    aggregate: str
    index: int
    method: str
    params_ref: str
    schema_title: str


def load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def write_json(path: Path, value: Any) -> None:
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def pascal_case(value: str) -> str:
    parts = re.findall(r"[A-Za-z0-9]+", value)
    return "".join(part[:1].upper() + part[1:] for part in parts)


def title_variant(title: str) -> str:
    for suffix in ("Request", "Notification"):
        if title.endswith(suffix):
            title = title[: -len(suffix)]
            break
    return pascal_case(title)


def ref_name(ref: str) -> str:
    if not ref:
        return ""
    return ref.rsplit("/", 1)[-1]


def type_name_from_schema(path: Path) -> str:
    data = load_json(path)
    return data.get("title") or path.stem


def schema_files_map(root: Path) -> dict[str, Path]:
    return {path.stem: path.relative_to(root) for path in root.rglob("*.json") if path.name not in schema_utils.METADATA_FILES}


def schema_path_for_type(root: Path, type_name: str) -> str:
    if not type_name:
        return ""
    by_file = schema_files_map(root)
    if type_name in by_file:
        return by_file[type_name].as_posix()
    for path in root.rglob("*.json"):
        if path.name in schema_utils.METADATA_FILES:
            continue
        try:
            if load_json(path).get("title") == type_name:
                return path.relative_to(root).as_posix()
        except json.JSONDecodeError:
            continue
    return ""


def response_type_name(response_type: str) -> str:
    value = response_type.strip()
    value = value.replace("crate::protocol::", "")
    value = value.replace("crate::", "")
    value = value.replace("super::", "")
    if "::" in value:
        value = value.rsplit("::", 1)[-1]
    return value


def find_matching_brace(text: str, open_index: int) -> int:
    depth = 0
    in_string = False
    escape = False
    for index in range(open_index, len(text)):
        char = text[index]
        if in_string:
            if escape:
                escape = False
            elif char == "\\":
                escape = True
            elif char == '"':
                in_string = False
            continue
        if char == '"':
            in_string = True
            continue
        if char == "{":
            depth += 1
            continue
        if char == "}":
            depth -= 1
            if depth == 0:
                return index
    raise ValueError("unbalanced braces while parsing common.rs")


def extract_macro_body(text: str, macro_name: str) -> str:
    marker = f"{macro_name}!"
    start = text.index(marker)
    open_index = text.index("{", start)
    close_index = find_matching_brace(text, open_index)
    return text[open_index + 1 : close_index]


def parse_request_mappings(common_rs: Path) -> dict[str, RequestMapping]:
    text = common_rs.read_text(encoding="utf-8")
    mappings: dict[str, RequestMapping] = {}
    for macro_name in ("client_request_definitions", "server_request_definitions"):
        body = extract_macro_body(text, macro_name)
        for match in re.finditer(r"(?ms)(?P<prefix>(?:\s*(?:#\[[^\n]*\]|///[^\n]*|//[^\n]*)\n)*)\s*(?P<variant>[A-Za-z][A-Za-z0-9_]*)(?:\s*=>\s*\"(?P<wire>[^\"]+)\")?\s*\{(?P<body>.*?)\n\s*\},", body):
            wire = match.group("wire")
            if not wire:
                continue
            entry_body = match.group("body")
            response_match = re.search(r"response:\s*([^,\n]+)", entry_body)
            if not response_match:
                continue
            mappings[wire] = RequestMapping(
                variant=match.group("variant"),
                response_type=response_type_name(response_match.group(1)),
                experimental="#[experimental" in match.group("prefix"),
                macro_name=macro_name,
            )
    return mappings


def load_common_rs_source_sha(common_rs: Path, explicit_sha: str) -> str:
    if explicit_sha:
        return explicit_sha
    sidecar = common_rs.with_name(f"{common_rs.name}.source_sha")
    if sidecar.exists():
        return sidecar.read_text(encoding="utf-8").strip()
    return ""


def verify_common_rs_provenance(common_rs: Path, source_sha: str, target_sha: str, codex_repo: Path | None = None) -> None:
    if not source_sha:
        raise ValueError("common.rs source SHA is required")
    if source_sha != target_sha:
        raise ValueError(f"common.rs source SHA {source_sha} does not match target {target_sha}")
    if codex_repo is None:
        return

    completed = subprocess.run(
        ["git", "-C", str(codex_repo), "show", f"{target_sha}:{COMMON_RS_REF}"],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if completed.stdout != common_rs.read_bytes():
        raise ValueError(f"common.rs content does not match {target_sha}:{COMMON_RS_REF}")


def aggregate_entries(root: Path, aggregate: str) -> list[AggregateEntry]:
    schema = load_json(root / aggregate)
    entries: list[AggregateEntry] = []
    for index, variant in enumerate(schema.get("oneOf", [])):
        properties = variant.get("properties", {})
        methods = properties.get("method", {}).get("enum", [])
        if not methods:
            continue
        params = properties.get("params", {})
        entries.append(
            AggregateEntry(
                aggregate=aggregate,
                index=index,
                method=methods[0],
                params_ref=params.get("$ref", ""),
                schema_title=variant.get("title", ""),
            )
        )
    return entries


def aggregate_kind_direction(aggregate: str) -> tuple[str, str]:
    if aggregate == "ClientRequest.json":
        return "client_to_server", "request"
    if aggregate == "ServerRequest.json":
        return "server_to_client", "request"
    if aggregate == "ServerNotification.json":
        return "server_to_client", "notification"
    if aggregate == "ClientNotification.json":
        return "client_to_server", "notification"
    raise ValueError(f"unsupported aggregate schema: {aggregate}")


def facade_target(entry: AggregateEntry, direction: str, kind: str, existing: dict[str, Any] | None, mapping: RequestMapping | None) -> str:
    if existing and existing.get("facade_target"):
        return existing["facade_target"]
    if direction == "server_to_client" and kind == "request":
        return f"ServerRequests().{mapping.variant if mapping else title_variant(entry.schema_title)}"
    if direction == "server_to_client" and kind == "notification":
        return f"ServerNotifications().{title_variant(entry.schema_title)}"
    if direction == "client_to_server" and kind == "notification":
        return f"ClientNotifications().{title_variant(entry.schema_title)}"
    family = entry.method.split("/", 1)[0]
    if entry.method in ROOT_INTERNAL_OVERRIDES:
        return ROOT_INTERNAL_OVERRIDES[entry.method]
    if family in ROOT_OPERATION_OVERRIDES:
        return ROOT_OPERATION_OVERRIDES[family]
    accessor = FAMILY_ACCESSORS.get(family, f"{pascal_case(family)}()")
    suffix = entry.method.split("/", 1)[1] if "/" in entry.method else entry.method
    return f"{accessor}.{pascal_case(suffix)}"


def build_manifest(root: Path, old_manifest: dict[str, Any], mappings: dict[str, RequestMapping], source_commit: str) -> dict[str, Any]:
    old_entries = {entry["method"]: entry for entry in old_manifest.get("entries", [])}
    aggregates = old_manifest.get("aggregate_schemas") or list(schema_utils.AGGREGATE_SCHEMAS)
    entries: list[dict[str, Any]] = []
    for aggregate in aggregates:
        direction, kind = aggregate_kind_direction(aggregate)
        for aggregate_entry in aggregate_entries(root, aggregate):
            existing = old_entries.get(aggregate_entry.method)
            mapping = mappings.get(aggregate_entry.method)
            params_name = ref_name(aggregate_entry.params_ref)
            params_schema = schema_path_for_type(root, params_name)
            stability = existing.get("stability") if existing else ("experimental" if mapping and mapping.experimental else "stable")
            stability_source = existing.get("stability_source") if existing else ("experimental_only_in_schema" if stability == "experimental" else "present_in_stable_schema")
            stability_text = "absent from schema generated without --experimental" if stability == "experimental" else "present in schema generated without --experimental"

            response_schema = ""
            response_status = "not_applicable"
            response_type = ""
            response_mapping = "not_applicable:notification_does_not_expect_response"
            if kind == "request":
                response_status = "declared"
                response_type = mapping.response_type if mapping else existing.get("response_type", "")
                response_schema = schema_path_for_type(root, response_type) or existing.get("response_schema", "")
                if not response_schema:
                    raise SystemExit(f"unable to resolve response schema for request method {aggregate_entry.method!r}")
                response_mapping = f"{COMMON_RS_REF}#{mapping.macro_name}/{mapping.variant}" if mapping else existing.get("source_ref", {}).get("response_mapping", "")
                if not response_mapping or response_mapping.startswith("not_applicable"):
                    raise SystemExit(f"missing response mapping for request method {aggregate_entry.method!r}")

            target = facade_target(aggregate_entry, direction, kind, existing, mapping)
            manifest_entry = {
                "direction": direction,
                "facade_target": target,
                "family": aggregate_entry.method.split("/", 1)[0],
                "kind": kind,
                "method": aggregate_entry.method,
                "params_or_payload_schema": params_name,
                "response_schema": response_schema,
                "response_schema_status": response_status,
                "response_type": response_type,
                "schema_title": aggregate_entry.schema_title,
                "source_ref": {
                    "aggregate_pointer": f"{aggregate}#/oneOf/{aggregate_entry.index}",
                    "aggregate_schema": aggregate,
                    "baseline_source_commit": source_commit,
                    "facade_rule": "manifest_generation.json#facade_target_rule",
                    "params_or_payload_schema": params_schema,
                    "response_mapping": response_mapping,
                    "response_schema": response_schema,
                    "stability": stability_text,
                },
                "source_schema": aggregate,
                "source_variant": mapping.variant if mapping else title_variant(aggregate_entry.schema_title),
                "stability": stability,
                "stability_source": stability_source,
            }
            if direction == "client_to_server" and kind == "request" and re.fullmatch(r"[A-Za-z][A-Za-z0-9]*\(\)\.[A-Za-z][A-Za-z0-9]*", target):
                manifest_entry["facade_status"] = existing.get("facade_status", "generated") if existing else "generated"
            entries.append(manifest_entry)
    return {
        "aggregate_schemas": aggregates,
        "classification_sources": old_manifest.get("classification_sources", []),
        "description": old_manifest.get("description", "Classified app-server protocol manifest."),
        "entries": sorted(entries, key=lambda item: item["method"]),
        "surface": old_manifest.get("surface", []),
        "schema_version": old_manifest.get("schema_version", 1),
        "status": "classified-manifest",
    }


def pointer_exists(root: Path, schema: str, path: str) -> bool:
    if not schema or not path:
        return False
    schema_path, pointer = schema_utils.split_schema_pointer(path)
    if schema_path != schema or not (root / schema).is_file():
        return False
    return schema_utils.json_pointer_exists(load_json(root / schema), pointer)


def default_method_coverage(manifest_entry: dict[str, Any]) -> dict[str, Any]:
    return {
        "direction": manifest_entry["direction"],
        "exit_condition": "Regenerate method registry and add handwritten facade coverage only if this upstream method becomes part of the public SDK surface.",
        "kind": manifest_entry["kind"],
        "method": manifest_entry["method"],
        "owner": "codex-go-sdk",
        "reason": f"Generated protocolv2 method registry coverage for {manifest_entry['direction']} {manifest_entry['kind']} method; no handwritten facade is claimed until explicitly added.",
        "revisit_trigger": "protocolv2 generation or upstream schema drift",
        "source_schema": manifest_entry["source_schema"],
        "stability": manifest_entry["stability"],
        "status": "supported-generated",
    }


def default_type_coverage(root: Path, schema: str, stability: str = "stable") -> dict[str, Any]:
    return {
        "exit_condition": "Regenerate if upstream schema drift changes this generated protocol type.",
        "owner": "codex-go-sdk",
        "reason": "Generated from the checked-in upstream app-server schema baseline.",
        "revisit_trigger": "protocolv2 generation or upstream schema drift",
        "schema": schema,
        "stability": stability,
        "status": "supported-generated",
        "type": type_name_from_schema(root / schema),
    }


def default_field_coverage(root: Path, schema: str, field: str, required: bool, stability: str = "stable") -> dict[str, Any]:
    typ = type_name_from_schema(root / schema)
    return {
        "exit_condition": f"Regenerate if upstream schema drift changes {typ}.{field} semantics.",
        "field": field,
        "owner": "codex-go-sdk",
        "path": f"{schema}#/properties/{field}",
        "reason": f"Generated as a strict typed {typ} field.",
        "required": required,
        "revisit_trigger": "protocolv2 generation or upstream schema drift",
        "schema": schema,
        "stability": stability,
        "status": "supported-generated",
        "type": typ,
    }


def top_level_object_fields(root: Path, schema: str) -> list[dict[str, Any]]:
    data = load_json(root / schema)
    if not isinstance(data, dict) or data.get("type") != "object":
        return []
    properties = data.get("properties", {})
    if not isinstance(properties, dict):
        return []
    required = set(data.get("required", []))
    return [
        default_field_coverage(root, schema, field, field in required)
        for field in sorted(properties)
    ]


def build_coverage(root: Path, old_coverage: dict[str, Any], manifest: dict[str, Any], field_seed_schemas: set[str]) -> dict[str, Any]:
    valid_statuses = old_coverage.get("valid_statuses") or ["supported", "supported-generated", "deferred", "intentionally-unsupported"]

    old_methods = {item["method"]: item for item in old_coverage.get("methods", [])}
    methods = []
    for entry in manifest["entries"]:
        method = dict(old_methods.get(entry["method"], default_method_coverage(entry)))
        method.update(
            {
                "direction": entry["direction"],
                "kind": entry["kind"],
                "method": entry["method"],
                "source_schema": entry["source_schema"],
                "stability": entry["stability"],
            }
        )
        methods.append(method)

    schema_paths = set(schema_utils.schema_files(root))
    old_types = {item["schema"]: item for item in old_coverage.get("types", [])}
    manifest_stability = {entry["response_schema"]: entry["stability"] for entry in manifest["entries"] if entry.get("response_schema")}
    for entry in manifest["entries"]:
        params_schema = schema_path_for_type(root, entry.get("params_or_payload_schema", ""))
        if params_schema:
            manifest_stability.setdefault(params_schema, entry["stability"])
    types = []
    for schema in sorted(schema_paths):
        typ = dict(old_types.get(schema, default_type_coverage(root, schema, manifest_stability.get(schema, "stable"))))
        typ.update({"schema": schema})
        if schema in manifest_stability:
            typ["stability"] = manifest_stability[schema]
        types.append(typ)

    old_fields = old_coverage.get("fields", [])
    fields_by_path: dict[str, dict[str, Any]] = {}
    for field in old_fields:
        if pointer_exists(root, field.get("schema", ""), field.get("path", "")):
            fields_by_path[field["path"]] = dict(field)
    for schema in sorted(field_seed_schemas):
        if schema not in schema_paths:
            continue
        stability = manifest_stability.get(schema, old_types.get(schema, {}).get("stability", "stable"))
        for field in top_level_object_fields(root, schema):
            field["stability"] = stability
            fields_by_path.setdefault(field["path"], field)

    return {
        "description": old_coverage.get("description", "Coverage classification for the checked-in Codex app-server protocol baseline."),
        "fields": sorted(fields_by_path.values(), key=lambda item: item["path"]),
        "methods": sorted(methods, key=lambda item: item["method"]),
        "schema_version": old_coverage.get("schema_version", 1),
        "status": "classified-manifest",
        "types": sorted(types, key=lambda item: item["schema"]),
        "valid_statuses": valid_statuses,
    }


def update_manifest_generation(path: Path, target_ref: str, target_kind: str, target_sha: str) -> None:
    if not path.exists():
        return
    data = load_json(path)
    inputs = data.setdefault("inputs", {})
    inputs["source_ref_name"] = target_ref
    inputs["source_ref_kind"] = target_kind
    inputs["source_commit"] = target_sha
    write_json(path, data)


def remove_checked_in_schema_files(root: Path) -> None:
    for rel in schema_utils.schema_files(root):
        (root / rel).unlink()


def copy_candidate_schema(candidate: Path, root: Path) -> None:
    remove_checked_in_schema_files(root)
    for rel in schema_utils.schema_files(candidate):
        source = candidate / rel
        dest = root / rel
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, dest)


def build_metadata(root: Path, old: dict[str, Any], target_ref: str, target_kind: str, target_sha: str, codex_version: str) -> dict[str, Any]:
    return {
        "aggregate_schemas": list(schema_utils.AGGREGATE_SCHEMAS),
        "codex_binary": "codex",
        "codex_version": codex_version,
        "experimental_included": True,
        "generated_at": datetime.now(UTC).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
        "generation_command": "codex app-server generate-json-schema --experimental --out codexsdk/internal/protocolschema/appserver/v2",
        "schema_bundle_sha256": schema_utils.schema_bundle_sha256(root),
        "schema_file_count": len(schema_utils.schema_files(root)),
        "schema_output_layout": "root JSON files plus v1/ and v2/",
        "schema_version": old.get("schema_version", 1),
        "source_commit": target_sha,
        "source_dirty": False,
        "source_license": "Apache-2.0",
        "source_ref_kind": target_kind,
        "source_ref_name": target_ref,
        "source_ref_url": f"https://github.com/openai/codex/tree/{target_sha}",
        "source_repo": "https://github.com/openai/codex",
        "source_schema_command_ref": "codex app-server generate-json-schema",
        "source_subdir": "codex-rs/app-server-protocol",
    }


def write_clean_reports(
    root: Path,
    candidate: Path,
    reports: Path,
    target_ref: str,
    target_kind: str,
    target_sha: str,
    codex_version: str,
    generated_compatibility: dict[str, Any],
) -> None:
    drift, matrix, _summary = schema_diff.build_reports(
        baseline=root,
        candidate=candidate,
        reports=reports,
        source_commit=target_sha,
        source_ref=target_ref,
        source_ref_kind=target_kind,
        codex_version=codex_version,
        generator="cargo",
        generator_detail="codex app-server generate-json-schema --experimental --out codexsdk/internal/protocolschema/appserver/v2",
    )
    drift["target"]["schema_bundle_sha256"] = schema_utils.schema_bundle_sha256(root)
    drift["generated_compatibility"] = generated_compatibility
    matrix["generated_compatibility_updates"] = {
        "added": generated_compatibility["added"],
        "removed": generated_compatibility["removed"],
        "reclassified": generated_compatibility["reclassified"],
        "changed": generated_compatibility["changed"],
    }
    write_json(root / "drift_report.json", drift)
    write_json(root / "matrix_update_skeleton.json", matrix)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", type=Path, default=DEFAULT_BASELINE, help="checked-in app-server v2 schema baseline root")
    parser.add_argument("--candidate", required=True, type=Path, help="trusted candidate schema directory")
    parser.add_argument("--stable-candidate", required=True, type=Path, help="schema generated from the same source without experimental visibility")
    parser.add_argument("--codex-repo", type=Path, help="local openai/codex clone used to verify common.rs content")
    parser.add_argument("--reports", type=Path, help="candidate drift report directory")
    parser.add_argument("--common-rs", required=True, type=Path, help="upstream common.rs response mapping source")
    parser.add_argument("--common-rs-source-sha", default="", help="commit SHA that produced --common-rs")
    parser.add_argument("--target-ref", required=True, help="selected upstream ref name")
    parser.add_argument("--target-kind", required=True, help="selected upstream ref kind")
    parser.add_argument("--target-sha", required=True, help="selected upstream commit SHA")
    parser.add_argument("--skip-codegen", action="store_true", help="do not regenerate protocolv2 Go files")
    parser.add_argument("--json", action="store_true", help="print a machine-readable summary")
    args = parser.parse_args()

    baseline = args.baseline
    candidate = args.candidate
    reports = args.reports or candidate.parent / "reports"
    if not baseline.is_dir():
        raise SystemExit(f"baseline directory does not exist: {baseline}")
    if not candidate.is_dir():
        raise SystemExit(f"candidate directory does not exist: {candidate}")
    if not args.stable_candidate.is_dir():
        raise SystemExit(f"stable candidate directory does not exist: {args.stable_candidate}")
    if not args.common_rs.is_file():
        raise SystemExit(f"common.rs does not exist: {args.common_rs}")
    try:
        common_rs_source_sha = load_common_rs_source_sha(args.common_rs, args.common_rs_source_sha)
        verify_common_rs_provenance(args.common_rs, common_rs_source_sha, args.target_sha, args.codex_repo)
    except (subprocess.CalledProcessError, ValueError) as exc:
        raise SystemExit(str(exc))

    old_metadata = load_json(baseline / "baseline_metadata.json")
    old_manifest = load_json(baseline / "manifest.json")
    old_coverage = load_json(baseline / "coverage_matrix.json")
    old_schema_files = set(schema_utils.schema_files(baseline))
    candidate_report = load_json(reports / "drift_summary.json") if (reports / "drift_summary.json").exists() else {}
    target_report = candidate_report.get("target", {})
    if target_report.get("source_commit") and target_report["source_commit"] != args.target_sha:
        raise SystemExit(f"candidate source_commit {target_report['source_commit']} does not match target {args.target_sha}")
    codex_version = target_report.get("codex_version") or f"codex-cli {args.target_ref.removeprefix('rust-v')}"

    copy_candidate_schema(candidate, baseline)
    candidate_schema_files = set(schema_utils.schema_files(baseline))
    file_diff = candidate_report.get("file_diff", {})
    added_schemas = set(file_diff.get("added", [])) or (candidate_schema_files - old_schema_files)
    changed_schemas = set(file_diff.get("changed", []))

    write_json(baseline / "baseline_metadata.json", build_metadata(baseline, old_metadata, args.target_ref, args.target_kind, args.target_sha, codex_version))
    update_manifest_generation(baseline / "manifest_generation.json", args.target_ref, args.target_kind, args.target_sha)

    mappings = parse_request_mappings(args.common_rs)
    manifest = build_manifest(baseline, old_manifest, mappings, args.target_sha)
    write_json(baseline / "manifest.json", manifest)
    coverage = build_coverage(baseline, old_coverage, manifest, added_schemas | changed_schemas)
    write_json(baseline / "coverage_matrix.json", coverage)
    classified_manifest.update_manifest(
        baseline / "manifest.json",
        classified_manifest.derive_surface(args.stable_candidate, baseline),
    )
    manifest = load_json(baseline / "manifest.json")
    generated_compatibility = release_report.compatibility_report(old_manifest, manifest)
    write_clean_reports(
        baseline,
        candidate,
        reports,
        args.target_ref,
        args.target_kind,
        args.target_sha,
        codex_version,
        generated_compatibility,
    )

    if not args.skip_codegen:
        subprocess.run(
            ["go", "run", "./codexsdk/internal/cmd/protocolv2gen"],
            cwd=Path.cwd(),
            env={**dict(os.environ), "GOWORK": "off"},
            check=True,
        )
        subprocess.run(
            ["python3", "scripts/codexsdk_generate_sdk_surface.py"],
            cwd=Path.cwd(),
            check=True,
        )

    summary = {
        "status": "ok",
        "added_schemas": sorted(added_schemas),
        "common_rs_source_sha": common_rs_source_sha,
        "schema_file_count": len(schema_utils.schema_files(baseline)),
        "method_count": len(manifest["entries"]),
        "coverage_type_count": len(coverage["types"]),
        "coverage_field_count": len(coverage["fields"]),
        "classified_surface_count": len(manifest["surface"]),
        "generated_compatibility_impact": generated_compatibility["compatibility_impact"],
        "target_ref": args.target_ref,
        "target_sha": args.target_sha,
    }
    if args.json:
        print(json.dumps(summary, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

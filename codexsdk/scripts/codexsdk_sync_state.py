#!/usr/bin/env python3
"""Validate checked-in Codex SDK schema sync state without editing files."""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

import codexsdk_schema_utils as schema_utils


@dataclass(frozen=True)
class Finding:
    code: str
    path: str
    message: str


def finding(code: str, path: str, message: str) -> Finding:
    return Finding(code=code, path=path, message=message)


def schema_exists(root: Path, rel: str) -> bool:
    return bool(rel) and (root / rel).is_file()


def validate_metadata(root: Path, target_sha: str) -> list[Finding]:
    findings: list[Finding] = []
    path = root / "baseline_metadata.json"
    try:
        metadata = schema_utils.load_json(path)
    except FileNotFoundError:
        return [finding("missing_file", str(path), "baseline metadata is missing")]

    actual_count = len(schema_utils.schema_files(root))
    if metadata.get("schema_file_count") != actual_count:
        findings.append(
            finding(
                "metadata_schema_count",
                str(path),
                f"schema_file_count={metadata.get('schema_file_count')!r}, want {actual_count}",
            )
        )

    actual_checksum = schema_utils.schema_bundle_sha256(root)
    if metadata.get("schema_bundle_sha256") != actual_checksum:
        findings.append(
            finding(
                "metadata_schema_bundle_sha256",
                str(path),
                f"schema_bundle_sha256={metadata.get('schema_bundle_sha256')!r}, want {actual_checksum}",
            )
        )

    if target_sha and metadata.get("source_commit") != target_sha:
        findings.append(
            finding(
                "metadata_target_sha",
                str(path),
                f"source_commit={metadata.get('source_commit')!r}, want {target_sha}",
            )
        )
    return findings


def validate_drift_report(root: Path) -> list[Finding]:
    findings: list[Finding] = []
    metadata_path = root / "baseline_metadata.json"
    drift_path = root / "drift_report.json"
    matrix_path = root / "matrix_update_skeleton.json"
    try:
        metadata = schema_utils.load_json(metadata_path)
        drift = schema_utils.load_json(drift_path)
    except FileNotFoundError as exc:
        return [finding("missing_file", str(exc.filename), "required sync report is missing")]

    if drift.get("status") != "clean":
        findings.append(finding("drift_report_dirty", str(drift_path), f"status={drift.get('status')!r}, want 'clean'"))

    for key in ("added", "changed", "removed"):
        values = drift.get("file_diff", {}).get(key, [])
        if values:
            findings.append(finding("drift_report_file_diff", str(drift_path), f"file_diff.{key} is not empty: {values!r}"))

    for schema, diff in drift.get("method_diff", {}).items():
        if diff.get("added") or diff.get("removed"):
            findings.append(finding("drift_report_method_diff", str(drift_path), f"{schema} has method drift: {diff!r}"))

    target = drift.get("target", {})
    for drift_key, metadata_key in (
        ("source_commit", "source_commit"),
        ("source_ref_name", "source_ref_name"),
        ("source_ref_kind", "source_ref_kind"),
    ):
        if target.get(drift_key) != metadata.get(metadata_key):
            findings.append(
                finding(
                    "drift_report_target_mismatch",
                    str(drift_path),
                    f"target.{drift_key}={target.get(drift_key)!r}, want metadata {metadata_key}={metadata.get(metadata_key)!r}",
                )
            )
    if target.get("schema_bundle_sha256") and target.get("schema_bundle_sha256") != metadata.get("schema_bundle_sha256"):
        findings.append(
            finding(
                "drift_report_checksum_mismatch",
                str(drift_path),
                "target.schema_bundle_sha256 does not match baseline_metadata.json",
            )
        )

    if matrix_path.exists():
        matrix = schema_utils.load_json(matrix_path)
        if matrix.get("status") != "empty":
            findings.append(finding("matrix_update_skeleton_dirty", str(matrix_path), f"status={matrix.get('status')!r}, want 'empty'"))
        for key in ("method_updates", "type_updates", "field_updates"):
            if matrix.get(key):
                findings.append(finding("matrix_update_skeleton_dirty", str(matrix_path), f"{key} is not empty"))
    else:
        findings.append(finding("missing_file", str(matrix_path), "matrix update skeleton is missing"))

    return findings


def validate_local_paths(root: Path) -> list[Finding]:
    findings: list[Finding] = []
    for rel in ("baseline_metadata.json", "drift_report.json", "matrix_update_skeleton.json"):
        path = root / rel
        if not path.exists():
            continue
        for json_path, value in schema_utils.find_local_paths(schema_utils.load_json(path)):
            findings.append(finding("local_path", f"{path}:{json_path}", f"contains local path value {value!r}"))
    return findings


def validate_manifest(root: Path) -> list[Finding]:
    findings: list[Finding] = []
    manifest_path = root / "manifest.json"
    try:
        manifest = schema_utils.load_json(manifest_path)
    except FileNotFoundError:
        return [finding("missing_file", str(manifest_path), "manifest is missing")]

    aggregates = manifest.get("aggregate_schemas") or list(schema_utils.AGGREGATE_SCHEMAS)
    aggregate_methods = schema_utils.aggregate_methods(root, aggregates)
    manifest_entries = manifest.get("entries", [])
    manifest_methods: dict[str, dict[str, Any]] = {}
    for entry in manifest_entries:
        method = entry.get("method", "")
        if not method:
            findings.append(finding("manifest_missing_method", str(manifest_path), f"manifest entry is missing method: {entry!r}"))
            continue
        if method in manifest_methods:
            findings.append(finding("manifest_duplicate_method", str(manifest_path), f"duplicate manifest method {method!r}"))
        manifest_methods[method] = entry

        facade_target = entry.get("facade_target", "")
        if entry.get("direction") == "client_to_server" and entry.get("kind") == "request":
            if facade_target.startswith("internal."):
                pass
            elif not re.fullmatch(r"[A-Za-z][A-Za-z0-9]*\(\)\.[A-Za-z][A-Za-z0-9]*", facade_target):
                findings.append(finding("manifest_invalid_facade_target", str(manifest_path), f"{method}: invalid facade_target {facade_target!r}"))
            else:
                facade_status = entry.get("facade_status", "")
                if facade_status not in {"generated", "deferred_missing_generated_types"}:
                    findings.append(finding("manifest_invalid_facade_status", str(manifest_path), f"{method}: invalid facade_status {facade_status!r}"))

        source_schema = entry.get("source_schema", "")
        if not schema_exists(root, source_schema):
            findings.append(finding("manifest_missing_source_schema", str(manifest_path), f"{method}: source_schema {source_schema!r} does not exist"))

        response_schema = entry.get("response_schema", "")
        if response_schema and not schema_exists(root, response_schema):
            findings.append(finding("manifest_missing_response_schema", str(manifest_path), f"{method}: response_schema {response_schema!r} does not exist"))

    for method, aggregate in sorted(aggregate_methods.items()):
        entry = manifest_methods.get(method)
        if entry is None:
            findings.append(finding("manifest_missing_aggregate_method", str(manifest_path), f"manifest missing aggregate method {method!r}"))
            continue
        expected_params = aggregate["params_or_payload_schema"]
        if entry.get("params_or_payload_schema", "") != expected_params:
            findings.append(
                finding(
                    "manifest_params_mismatch",
                    str(manifest_path),
                    f"{method}: params_or_payload_schema={entry.get('params_or_payload_schema')!r}, want {expected_params!r}",
                )
            )

    for method in sorted(set(manifest_methods) - set(aggregate_methods)):
        findings.append(finding("manifest_stale_method", str(manifest_path), f"manifest method {method!r} is not present in aggregate schemas"))

    if int(manifest.get("schema_version", 1)) >= 2:
        surface = manifest.get("surface", [])
        if not surface:
            findings.append(finding("manifest_empty_surface", str(manifest_path), "schema v2 manifest has no classified generated surface"))
        seen_surface: set[tuple[str, str]] = set()
        valid_kinds = {"const", "field", "func", "interface_method", "method", "type", "value", "var"}
        valid_stability = {"stable", "experimental", "mixed"}
        for entry in surface:
            kind = entry.get("kind", "")
            name = entry.get("name", "")
            stability = entry.get("stability", "")
            signature = entry.get("signature", "")
            identity = (kind, name)
            if not kind or not name or not signature or stability not in valid_stability:
                findings.append(finding("manifest_unclassified_surface", str(manifest_path), f"invalid surface entry: {entry!r}"))
                continue
            if kind not in valid_kinds or (stability == "mixed" and kind != "type"):
                findings.append(finding("manifest_invalid_surface", str(manifest_path), f"invalid classified surface entry: {entry!r}"))
            if identity in seen_surface:
                findings.append(finding("manifest_duplicate_surface", str(manifest_path), f"duplicate surface identity {kind} {name!r}"))
            seen_surface.add(identity)
    return findings


def validate_coverage(root: Path) -> list[Finding]:
    findings: list[Finding] = []
    manifest_path = root / "manifest.json"
    coverage_path = root / "coverage_matrix.json"
    try:
        manifest = schema_utils.load_json(manifest_path)
        coverage = schema_utils.load_json(coverage_path)
    except FileNotFoundError as exc:
        return [finding("missing_file", str(exc.filename), "required manifest or coverage file is missing")]

    manifest_methods = {entry.get("method", "") for entry in manifest.get("entries", []) if entry.get("method")}

    for entry in coverage.get("methods", []):
        method = entry.get("method", "")
        if method not in manifest_methods:
            findings.append(finding("coverage_stale_method", str(coverage_path), f"coverage method {method!r} is not present in manifest"))
        source_schema = entry.get("source_schema", "")
        if source_schema and not schema_exists(root, source_schema):
            findings.append(finding("coverage_missing_source_schema", str(coverage_path), f"{method}: source_schema {source_schema!r} does not exist"))

    for entry in coverage.get("types", []):
        schema = entry.get("schema", "")
        if not schema_exists(root, schema):
            findings.append(finding("coverage_stale_type_schema", str(coverage_path), f"coverage type schema {schema!r} does not exist"))

    schema_cache: dict[str, Any] = {}
    for entry in coverage.get("fields", []):
        schema = entry.get("schema", "")
        path = entry.get("path", "")
        path_schema, pointer = schema_utils.split_schema_pointer(path)
        if path_schema != schema:
            findings.append(finding("coverage_field_path_schema_mismatch", str(coverage_path), f"field path {path!r} does not match schema {schema!r}"))
            continue
        if not schema_exists(root, schema):
            findings.append(finding("coverage_stale_field_schema", str(coverage_path), f"coverage field schema {schema!r} does not exist"))
            continue
        if schema not in schema_cache:
            schema_cache[schema] = schema_utils.load_json(root / schema)
        if not schema_utils.json_pointer_exists(schema_cache[schema], pointer):
            findings.append(finding("coverage_stale_field_pointer", str(coverage_path), f"coverage field path {path!r} does not exist"))

    return findings


def validate_candidate(root: Path, candidate: Path | None) -> list[Finding]:
    if candidate is None:
        return []
    diff = schema_utils.schema_diff(root, candidate)
    method_diff = schema_utils.aggregate_method_diff(root, candidate)
    findings: list[Finding] = []
    if diff["added"] or diff["changed"] or diff["removed"]:
        findings.append(finding("candidate_schema_mismatch", str(candidate), f"candidate schema diff is not clean: {diff!r}"))
    dirty_methods = {schema: value for schema, value in method_diff.items() if value["added"] or value["removed"]}
    if dirty_methods:
        findings.append(finding("candidate_method_mismatch", str(candidate), f"candidate method diff is not clean: {dirty_methods!r}"))
    return findings


def validate_baseline(root: Path, *, candidate: Path | None = None, target_sha: str = "") -> list[Finding]:
    findings: list[Finding] = []
    findings.extend(validate_metadata(root, target_sha))
    findings.extend(validate_drift_report(root))
    findings.extend(validate_local_paths(root))
    findings.extend(validate_manifest(root))
    findings.extend(validate_coverage(root))
    findings.extend(validate_candidate(root, candidate))
    return findings


def existing_dir(value: str) -> Path:
    path = Path(value)
    if not path.is_dir():
        raise argparse.ArgumentTypeError(f"directory does not exist: {value}")
    return path


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", required=True, type=existing_dir, help="checked-in schema baseline directory")
    parser.add_argument("--candidate", type=existing_dir, help="candidate schema directory that should match the baseline")
    parser.add_argument("--target-sha", default="", help="expected baseline source_commit")
    parser.add_argument("--json", action="store_true", help="print machine-readable findings")
    args = parser.parse_args()

    findings = validate_baseline(args.baseline, candidate=args.candidate, target_sha=args.target_sha)
    payload = {
        "status": "ok" if not findings else "failed",
        "finding_count": len(findings),
        "findings": [asdict(item) for item in findings],
    }
    if args.json:
        print(json.dumps(payload, sort_keys=True))
    elif findings:
        print(f"sync-state: {len(findings)} finding(s)", file=sys.stderr)
        for item in findings:
            print(f"- {item.code} {item.path}: {item.message}", file=sys.stderr)
    return 1 if findings else 0


if __name__ == "__main__":
    raise SystemExit(main())

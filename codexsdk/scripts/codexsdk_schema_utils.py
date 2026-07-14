#!/usr/bin/env python3
"""Shared helpers for Codex SDK schema sync scripts."""

from __future__ import annotations

import hashlib
import json
import re
from pathlib import Path
from typing import Any


METADATA_FILES = {
    "baseline_metadata.json",
    "coverage_matrix.json",
    "drift_report.json",
    "manifest.json",
    "manifest_generation.json",
    "matrix_update_skeleton.json",
}

AGGREGATE_SCHEMAS = (
    "ClientRequest.json",
    "ServerRequest.json",
    "ServerNotification.json",
    "ClientNotification.json",
)

LOCAL_PATH_RE = re.compile(r"(^|[\s\"'=])(/Users/|/home/|/private/|/tmp/|/var/folders/|[A-Za-z]:\\|\\\\)")
LOCAL_CACHE_MARKERS = (
    ".cache/codexsdk-upstream",
    ".cache/openai-codex",
)


def load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def canonical_json(value: Any) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode("utf-8")


def schema_files(root: Path) -> list[str]:
    files: list[str] = []
    for path in root.rglob("*.json"):
        if path.name in METADATA_FILES:
            continue
        files.append(path.relative_to(root).as_posix())
    return sorted(files)


def schema_hashes(root: Path) -> dict[str, str]:
    hashes: dict[str, str] = {}
    for rel in schema_files(root):
        value = load_json(root / rel)
        hashes[rel] = hashlib.sha256(canonical_json(value)).hexdigest()
    return hashes


def schema_bundle_sha256(root: Path) -> str:
    return hashlib.sha256(canonical_json(schema_hashes(root))).hexdigest()


def ref_name(ref: str) -> str:
    if not ref:
        return ""
    return ref.rsplit("/", 1)[-1]


def aggregate_method_entries(root: Path, rel: str) -> dict[str, str]:
    path = root / rel
    if not path.exists():
        return {}
    schema = load_json(path)
    entries: dict[str, str] = {}
    for item in schema.get("oneOf", []):
        properties = item.get("properties", {})
        enum = properties.get("method", {}).get("enum", [])
        if not enum:
            continue
        params_ref = properties.get("params", {}).get("$ref", "")
        payload_ref = properties.get("payload", {}).get("$ref", "")
        entries[enum[0]] = ref_name(params_ref or payload_ref)
    return entries


def aggregate_method_names(root: Path, rel: str) -> set[str]:
    return set(aggregate_method_entries(root, rel))


def aggregate_methods(root: Path, aggregates: list[str] | tuple[str, ...] = AGGREGATE_SCHEMAS) -> dict[str, dict[str, str]]:
    methods: dict[str, dict[str, str]] = {}
    for aggregate in aggregates:
        for method, params_schema in aggregate_method_entries(root, aggregate).items():
            methods[method] = {
                "params_or_payload_schema": params_schema,
                "source_schema": aggregate,
            }
    return methods


def schema_diff(baseline: Path, candidate: Path) -> dict[str, list[str]]:
    base = schema_hashes(baseline)
    cand = schema_hashes(candidate)
    return {
        "added": sorted(set(cand) - set(base)),
        "changed": sorted(path for path in set(base) & set(cand) if base[path] != cand[path]),
        "removed": sorted(set(base) - set(cand)),
    }


def aggregate_method_diff(baseline: Path, candidate: Path) -> dict[str, dict[str, list[str]]]:
    diff: dict[str, dict[str, list[str]]] = {}
    for rel in AGGREGATE_SCHEMAS:
        before = aggregate_method_names(baseline, rel)
        after = aggregate_method_names(candidate, rel)
        diff[rel] = {
            "added": sorted(after - before),
            "removed": sorted(before - after),
        }
    return diff


def split_schema_pointer(value: str) -> tuple[str, str]:
    if "#" not in value:
        return value, ""
    schema, pointer = value.split("#", 1)
    return schema, pointer


def json_pointer_exists(document: Any, pointer: str) -> bool:
    if pointer in {"", "#"}:
        return True
    if pointer.startswith("#"):
        pointer = pointer[1:]
    if not pointer.startswith("/"):
        return False

    current = document
    for raw_part in pointer.split("/")[1:]:
        part = raw_part.replace("~1", "/").replace("~0", "~")
        if isinstance(current, dict):
            if part not in current:
                return False
            current = current[part]
            continue
        if isinstance(current, list):
            if not part.isdigit():
                return False
            index = int(part)
            if index >= len(current):
                return False
            current = current[index]
            continue
        return False
    return True


def find_local_paths(value: Any, path: str = "$") -> list[tuple[str, str]]:
    findings: list[tuple[str, str]] = []
    if isinstance(value, dict):
        for key, item in value.items():
            findings.extend(find_local_paths(item, f"{path}.{key}"))
        return findings
    if isinstance(value, list):
        for index, item in enumerate(value):
            findings.extend(find_local_paths(item, f"{path}[{index}]"))
        return findings
    if isinstance(value, str):
        if LOCAL_PATH_RE.search(value) or any(marker in value for marker in LOCAL_CACHE_MARKERS):
            findings.append((path, value))
    return findings

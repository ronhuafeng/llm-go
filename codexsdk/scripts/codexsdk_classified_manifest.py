#!/usr/bin/env python3
"""Derive the generated Go compatibility surface from schema visibility."""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import tempfile
from pathlib import Path
from typing import Any


REPO = Path(__file__).resolve().parents[1]


def load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def pointer_exists(root: Path, path: str) -> bool:
    schema_name, separator, pointer = path.partition("#")
    if not separator or not (root / schema_name).is_file() or not pointer.startswith("/"):
        return False
    value: Any = load_json(root / schema_name)
    for encoded in pointer[1:].split("/"):
        token = encoded.replace("~1", "/").replace("~0", "~")
        if isinstance(value, dict) and token in value:
            value = value[token]
        elif isinstance(value, list) and token.isdigit() and int(token) < len(value):
            value = value[int(token)]
        else:
            return False
    return True


def prepare_generation_root(source: Path, complete: Path, destination: Path) -> None:
    shutil.copytree(source, destination)
    coverage = load_json(complete / "coverage_matrix.json")
    schemas = {path.relative_to(destination).as_posix() for path in destination.rglob("*.json")}
    coverage["types"] = [item for item in coverage.get("types", []) if item.get("schema") in schemas]
    coverage["fields"] = [
        item
        for item in coverage.get("fields", [])
        if item.get("schema") in schemas and pointer_exists(destination, item.get("path", ""))
    ]
    write_json(destination / "coverage_matrix.json", coverage)

    complete_manifest = load_json(complete / "manifest.json")
    stable_methods = {
        entry.get("method")
        for entry in complete_manifest.get("entries", [])
        if entry.get("stability") == "stable"
    }
    complete_manifest["entries"] = [
        entry for entry in complete_manifest.get("entries", []) if entry.get("method") in stable_methods
    ]
    # The method registry does not consume surface metadata. This valid seed is
    # replaced by the derived surface before anything is checked in.
    complete_manifest["surface"] = [
        {"kind": "type", "name": "SurfaceSeed", "signature": "struct{}", "stability": "stable"}
    ]
    write_json(destination / "manifest.json", complete_manifest)


def prepare_complete_generation_root(source: Path, destination: Path) -> None:
    shutil.copytree(source, destination)
    manifest = load_json(destination / "manifest.json")
    manifest["surface"] = [
        {"kind": "type", "name": "SurfaceSeed", "signature": "struct{}", "stability": "stable"}
    ]
    write_json(destination / "manifest.json", manifest)


def generate_package(schema_root: Path, output: Path) -> Path:
    subprocess.run(
        [
            "go",
            "run",
            "./codexsdk/internal/cmd/protocolv2gen",
            "-schema-root",
            str(schema_root),
            "-out",
            str(output),
        ],
        cwd=REPO,
        check=True,
    )
    return output


def classify_surface(stable_source: Path, complete_source: Path) -> list[dict[str, str]]:
    completed = subprocess.run(
        [
            "go",
            "run",
            "./codexsdk/internal/cmd/protocolv2gen",
            "-stdout",
            "classified-surface",
            "-stable-source",
            str(stable_source),
            "-complete-source",
            str(complete_source),
        ],
        cwd=REPO,
        check=True,
        stdout=subprocess.PIPE,
    )
    return json.loads(completed.stdout)


def derive_surface(stable_schema: Path, complete_schema: Path) -> list[dict[str, str]]:
    with tempfile.TemporaryDirectory() as raw_tmp:
        tmp = Path(raw_tmp)
        stable_root = tmp / "stable-schema"
        complete_root = tmp / "complete-schema"
        prepare_generation_root(stable_schema, complete_schema, stable_root)
        prepare_complete_generation_root(complete_schema, complete_root)
        stable_source = generate_package(stable_root, tmp / "stable-go")
        complete_source = generate_package(complete_root, tmp / "complete-go")
        return classify_surface(stable_source, complete_source)


def update_manifest(manifest_path: Path, surface: list[dict[str, str]]) -> None:
    manifest = load_json(manifest_path)
    manifest["surface"] = surface
    classification_sources = manifest.setdefault("classification_sources", {})
    classification_sources["generated_surface"] = (
        "exported Go identities compared between generation without and with experimental schema visibility"
    )
    manifest["schema_version"] = max(2, int(manifest.get("schema_version", 1)))
    write_json(manifest_path, manifest)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--stable-schema", required=True, type=Path)
    parser.add_argument("--complete-schema", required=True, type=Path)
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()

    surface = derive_surface(args.stable_schema, args.complete_schema)
    if args.manifest:
        update_manifest(args.manifest, surface)
    if args.json:
        print(json.dumps(surface, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

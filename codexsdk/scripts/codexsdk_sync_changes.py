#!/usr/bin/env python3
"""Capture, validate, and stage the bounded protocol sync change set."""

from __future__ import annotations

import argparse
import json
import subprocess
from pathlib import Path, PurePosixPath


MECHANICAL_PREFIX = PurePosixPath("codexsdk/internal/protocolschema/appserver/v2")


def git_bytes(repo_root: Path, *args: str) -> bytes:
    return subprocess.check_output(["git", "-C", str(repo_root), *args])


def changed_paths(repo_root: Path) -> list[str]:
    tracked = git_bytes(repo_root, "diff", "--name-only", "--no-renames", "-z", "HEAD", "--")
    untracked = git_bytes(repo_root, "ls-files", "--others", "--exclude-standard", "-z")
    return sorted({path.decode("utf-8") for path in (tracked + untracked).split(b"\0") if path})


def is_mechanical_path(path: str) -> bool:
    candidate = PurePosixPath(path)
    if candidate == MECHANICAL_PREFIX or MECHANICAL_PREFIX in candidate.parents:
        return True
    if candidate == PurePosixPath("codexsdk/sdk_surface.gen.go"):
        return True
    return candidate.parent == PurePosixPath("codexsdk/protocolv2") and candidate.name.endswith(".gen.go")


def validate_paths(paths: list[str], phase: str) -> None:
    if phase == "mechanical":
        invalid = [path for path in paths if not is_mechanical_path(path)]
    else:
        invalid = [
            path
            for path in paths
            if not path.startswith("codexsdk/")
            or path.startswith("codexsdk/.cache/")
            or path.startswith("codexsdk/.agents/")
        ]
    if invalid:
        rendered = "\n".join(f"- {path}" for path in invalid)
        raise ValueError(f"sync changes escape the {phase} scope:\n{rendered}")


def capture(repo_root: Path, output: Path, phase: str) -> dict[str, object]:
    paths = changed_paths(repo_root)
    validate_paths(paths, phase)
    payload: dict[str, object] = {"phase": phase, "paths": paths}
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return payload


def assert_clean(repo_root: Path) -> None:
    paths = changed_paths(repo_root)
    if paths:
        rendered = "\n".join(f"- {path}" for path in paths)
        raise ValueError(f"sync worktree must be clean before apply:\n{rendered}")


def stage(repo_root: Path, manifest_path: Path) -> None:
    payload = json.loads(manifest_path.read_text(encoding="utf-8"))
    phase = payload.get("phase")
    paths = payload.get("paths")
    if phase not in {"mechanical", "final"} or not isinstance(paths, list) or not all(
        isinstance(path, str) for path in paths
    ):
        raise ValueError("invalid sync change manifest")
    validate_paths(paths, phase)
    live_paths = changed_paths(repo_root)
    if live_paths != paths:
        raise ValueError(
            "sync changes changed since the manifest was captured; capture and validate the final set again"
        )
    if not paths:
        raise ValueError("sync change manifest is empty")
    pathspec = b"\0".join(path.encode("utf-8") for path in paths) + b"\0"
    subprocess.run(
        [
            "git",
            "-C",
            str(repo_root),
            "add",
            "-A",
            "--pathspec-from-file=-",
            "--pathspec-file-nul",
        ],
        input=pathspec,
        check=True,
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    clean_parser = subparsers.add_parser("assert-clean")
    clean_parser.add_argument("--repo-root", required=True, type=Path)
    capture_parser = subparsers.add_parser("capture")
    capture_parser.add_argument("--repo-root", required=True, type=Path)
    capture_parser.add_argument("--phase", choices=("mechanical", "final"), required=True)
    capture_parser.add_argument("--output", required=True, type=Path)
    stage_parser = subparsers.add_parser("stage")
    stage_parser.add_argument("--repo-root", required=True, type=Path)
    stage_parser.add_argument("--manifest", required=True, type=Path)
    args = parser.parse_args()

    if args.command == "assert-clean":
        assert_clean(args.repo_root.resolve())
    elif args.command == "capture":
        capture(args.repo_root.resolve(), args.output.resolve(), args.phase)
    elif args.command == "stage":
        stage(args.repo_root.resolve(), args.manifest.resolve())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

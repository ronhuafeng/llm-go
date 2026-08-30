#!/usr/bin/env python3
"""Create annotated codexsdk tags for stable upstream Codex baselines."""

from __future__ import annotations

import argparse
import json
import re
import shlex
import subprocess
import sys
from dataclasses import dataclass
from typing import Any


METADATA_PATH = "internal/protocolschema/appserver/v2/baseline_metadata.json"
PREFIX = "upstream-codex"
RUST_TAG_RE = re.compile(r"^rust-v[0-9]+[.][0-9]+[.][0-9]+$")


@dataclass(frozen=True)
class TagChoice:
    tag_name: str
    action: str
    reason: str


def git_output(args: list[str]) -> str:
    completed = subprocess.run(
        ["git", *args],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    return completed.stdout.strip()


def git_tag_commit(ref: str) -> str | None:
    completed = subprocess.run(
        ["git", "rev-parse", "--verify", "-q", f"{ref}^{{}}"],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    if completed.returncode != 0:
        return None
    return completed.stdout.strip()


def metadata_git_specs() -> tuple[str, ...]:
    return (f"HEAD:./{METADATA_PATH}", f"HEAD:codexsdk/{METADATA_PATH}")


def load_head_metadata() -> dict[str, Any]:
    last: subprocess.CompletedProcess[str] | None = None
    for spec in metadata_git_specs():
        completed = subprocess.run(
            ["git", "show", spec],
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        if completed.returncode == 0:
            return json.loads(completed.stdout)
        last = completed
    assert last is not None
    raise subprocess.CalledProcessError(
        last.returncode,
        last.args,
        output=last.stdout,
        stderr=last.stderr,
    )


def head_commit() -> str:
    return git_output(["rev-parse", "--verify", "HEAD^{commit}"])


def tag_name(metadata: dict[str, Any]) -> str:
    ref_name = str(metadata.get("source_ref_name") or "")
    ref_kind = str(metadata.get("source_ref_kind") or "")
    if ref_kind != "stable_rust_tag":
        raise ValueError(f"upstream sync tags require stable_rust_tag baseline, got {ref_kind or '<missing>'}")
    if not RUST_TAG_RE.fullmatch(ref_name):
        raise ValueError(f"stable source_ref_name is not a rust-vX.Y.Z tag: {ref_name}")
    return f"{PREFIX}-{ref_name}"


def sync_tag_message(metadata: dict[str, Any], codexsdk_commit: str) -> str:
    ref_name = str(metadata.get("source_ref_name") or "")
    lines = [
        f"Sync llm-go/codexsdk with openai/codex {ref_name}",
        "",
        f"upstream_repo: {metadata.get('source_repo', 'https://github.com/openai/codex')}",
        f"upstream_ref: {ref_name}",
        f"upstream_ref_kind: {metadata.get('source_ref_kind', '')}",
        f"upstream_commit: {metadata.get('source_commit', '')}",
        f"schema_bundle_sha256: {metadata.get('schema_bundle_sha256', '')}",
        f"codex_version: {metadata.get('codex_version', '')}",
        f"codexsdk_commit: {codexsdk_commit}",
    ]
    return "\n".join(lines).rstrip() + "\n"


def local_sync_tag_commit(base_tag: str) -> str | None:
    return git_tag_commit(f"refs/tags/{base_tag}")


def choose_tag(base_tag: str, existing_commit: str | None, commit: str) -> TagChoice:
    if existing_commit is None:
        return TagChoice(base_tag, "create", "base upstream sync tag is available")
    if existing_commit == commit:
        return TagChoice(base_tag, "exists", "base upstream sync tag already points at HEAD")
    return TagChoice(base_tag, "block", "base upstream sync tag already exists at a different commit")


def tag_collision_diagnostic(tag: str, existing_commit: str, requested_commit: str) -> str:
    return (
        f"refusing to create {tag}: existing tag resolves to {existing_commit}, "
        f"but HEAD resolves to {requested_commit}; no fallback tag will be created"
    )


def remote_tag_commit(remote: str, tag: str) -> str | None:
    output = git_output(["ls-remote", "--tags", remote, f"refs/tags/{tag}^{{}}", f"refs/tags/{tag}"])
    for line in output.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1] == f"refs/tags/{tag}^{{}}":
            return parts[0]
    for line in output.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1] == f"refs/tags/{tag}":
            return parts[0]
    return None


def create_tag(tag: str, commit: str, message: str) -> None:
    subprocess.run(
        ["git", "tag", "-a", tag, commit, "-m", message],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


def push_tag(remote: str, tag: str) -> None:
    subprocess.run(
        ["git", "push", remote, f"refs/tags/{tag}:refs/tags/{tag}"],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--create", action="store_true", help="create the annotated tag on HEAD")
    parser.add_argument("--push", nargs="?", const="origin", help="push the tag to the given remote; requires --create")
    parser.add_argument("--json", action="store_true", help="print machine-readable output")
    args = parser.parse_args()

    if args.push and not args.create:
        parser.error("--push requires --create")

    metadata = load_head_metadata()
    commit = head_commit()
    base_tag = tag_name(metadata)
    existing_commit = local_sync_tag_commit(base_tag)
    remote_commit = ""
    if args.push:
        remote_commit = remote_tag_commit(args.push, base_tag) or ""
        if remote_commit:
            existing_commit = remote_commit
    choice = choose_tag(base_tag, existing_commit, commit)
    message = sync_tag_message(metadata, commit)

    payload = {
        "action": choice.action,
        "codexsdk_commit": commit,
        "reason": choice.reason,
        "tag_name": choice.tag_name,
        "tag_message": message,
        "upstream_commit": metadata.get("source_commit", ""),
        "upstream_ref_kind": metadata.get("source_ref_kind", ""),
        "upstream_ref_name": metadata.get("source_ref_name", ""),
    }

    if choice.action == "block":
        assert existing_commit is not None
        payload["existing_commit"] = existing_commit
        if args.json:
            print(json.dumps(payload, indent=2, sort_keys=True))
        if args.create:
            print(tag_collision_diagnostic(choice.tag_name, existing_commit, commit), file=sys.stderr)
        return 1 if args.create else 0

    if args.create and choice.action == "create":
        create_tag(choice.tag_name, commit, message)
        payload["action"] = "created"
    elif args.create and choice.action == "exists":
        payload["action"] = "exists"

    if args.push and remote_commit == commit:
        payload["remote_already_current"] = True
    elif args.push:
        push_tag(args.push, choice.tag_name)
        payload["pushed_remote"] = args.push

    if args.json:
        print(json.dumps(payload, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except subprocess.CalledProcessError as exc:
        command = shlex.join(str(argument) for argument in exc.cmd)
        print(f"command failed with exit code {exc.returncode}: {command}", file=sys.stderr)
        if exc.stderr:
            print(exc.stderr, file=sys.stderr, end="" if exc.stderr.endswith("\n") else "\n")
        raise SystemExit(1)
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        raise SystemExit(1)

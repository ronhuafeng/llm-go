#!/usr/bin/env python3
"""Create annotated codexsdk-go tags for stable upstream Codex baselines."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from dataclasses import dataclass
from typing import Any


METADATA_PATH = "codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json"
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


def load_head_metadata() -> dict[str, Any]:
    raw = git_output(["show", f"HEAD:{METADATA_PATH}"])
    return json.loads(raw)


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
        f"Sync codexsdk-go with openai/codex {ref_name}",
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


def local_sync_tags(base_tag: str) -> dict[str, str]:
    tags = git_output(["tag", "--list", f"{base_tag}*"]).splitlines()
    out: dict[str, str] = {}
    for tag in tags:
        if is_sync_tag(base_tag, tag):
            commit = git_tag_commit(f"refs/tags/{tag}")
            if commit:
                out[tag] = commit
    return out


def is_sync_tag(base_tag: str, tag: str) -> bool:
    return tag == base_tag or re.fullmatch(re.escape(base_tag) + r"-sync[.][0-9]+", tag) is not None


def remote_sync_tags(remote: str, base_tag: str) -> dict[str, str]:
    output = git_output(
        [
            "ls-remote",
            "--tags",
            remote,
            f"refs/tags/{base_tag}",
            f"refs/tags/{base_tag}^{{}}",
            f"refs/tags/{base_tag}-sync.*",
            f"refs/tags/{base_tag}-sync.*^{{}}",
        ]
    )
    out: dict[str, str] = {}
    prefix = "refs/tags/"
    peel_suffix = "^{}"
    for line in output.splitlines():
        parts = line.split()
        if len(parts) != 2:
            continue
        sha, ref = parts
        peeled = ref.endswith(peel_suffix)
        if peeled:
            ref = ref[: -len(peel_suffix)]
        if not ref.startswith(prefix):
            continue
        tag = ref[len(prefix) :]
        if not is_sync_tag(base_tag, tag):
            continue
        if peeled or tag not in out:
            out[tag] = sha
    return out


def choose_tag(base_tag: str, existing_tags: dict[str, str], commit: str, next_suffix: bool) -> TagChoice:
    existing_commit = existing_tags.get(base_tag)
    if existing_commit is None:
        return TagChoice(base_tag, "create", "base upstream sync tag is available")
    if existing_commit == commit:
        return TagChoice(base_tag, "exists", "base upstream sync tag already points at HEAD")
    if not next_suffix:
        return TagChoice(
            base_tag,
            "block",
            "base upstream sync tag already exists at a different commit; use --next-suffix for a follow-up sync tag",
        )

    for index in range(2, 1000):
        candidate = f"{base_tag}-sync.{index}"
        existing_commit = existing_tags.get(candidate)
        if existing_commit is None:
            return TagChoice(candidate, "create", "using next follow-up sync tag suffix")
        if existing_commit == commit:
            return TagChoice(candidate, "exists", "follow-up upstream sync tag already points at HEAD")
    raise ValueError(f"too many follow-up sync tags for {base_tag}")


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
    parser.add_argument("--next-suffix", action="store_true", help="use -sync.N when the base sync tag already exists")
    parser.add_argument("--create", action="store_true", help="create the annotated tag on HEAD")
    parser.add_argument("--push", nargs="?", const="origin", help="push the tag to the given remote; requires --create")
    parser.add_argument("--json", action="store_true", help="print machine-readable output")
    args = parser.parse_args()

    if args.push and not args.create:
        parser.error("--push requires --create")

    metadata = load_head_metadata()
    commit = head_commit()
    base_tag = tag_name(metadata)
    existing_tags = local_sync_tags(base_tag)
    if args.push:
        existing_tags.update(remote_sync_tags(args.push, base_tag))
    choice = choose_tag(base_tag, existing_tags, commit, args.next_suffix)
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

    remote_commit = ""
    if args.push:
        remote_commit = remote_tag_commit(args.push, choice.tag_name) or ""
        if remote_commit and remote_commit != commit:
            payload["action"] = "block"
            payload["reason"] = "remote upstream sync tag already exists at a different commit"
            payload["remote_commit"] = remote_commit
            if args.json:
                print(json.dumps(payload, indent=2, sort_keys=True))
            else:
                print(payload["reason"], file=sys.stderr)
            return 1

    if choice.action == "block":
        if args.json:
            print(json.dumps(payload, indent=2, sort_keys=True))
        elif args.create:
            print(choice.reason, file=sys.stderr)
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
    except (subprocess.CalledProcessError, ValueError) as exc:
        print(str(exc), file=sys.stderr)
        raise SystemExit(1)

#!/usr/bin/env python3
"""Decide whether an upstream Codex target is safe to compare or sync."""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path
from typing import Any


RUST_TAG_RE = re.compile(r"^rust-v([0-9]+)[.]([0-9]+)[.]([0-9]+)$")
SHA_RE = re.compile(r"^[0-9a-f]{40}$")

STABLE_TAG = "stable_rust_tag"
MANUAL_REF = "manual_ref"
MANUAL_COMMIT = "manual_commit"
VALID_KINDS = {STABLE_TAG, MANUAL_REF, MANUAL_COMMIT}


def parse_bool(value: str) -> bool:
    if value.lower() in {"1", "true", "yes", "on"}:
        return True
    if value.lower() in {"0", "false", "no", "off"}:
        return False
    raise argparse.ArgumentTypeError(f"invalid boolean value: {value!r}")


def normalize_kind(kind: str, ref_name: str) -> str:
    if kind == "latest_stable_rust_tag":
        return STABLE_TAG
    if kind == "manual":
        return infer_kind(ref_name)
    if kind:
        return kind
    return infer_kind(ref_name)


def infer_kind(ref_name: str) -> str:
    if RUST_TAG_RE.fullmatch(ref_name):
        return STABLE_TAG
    if SHA_RE.fullmatch(ref_name):
        return MANUAL_COMMIT
    return MANUAL_REF


def parse_rust_tag(ref_name: str) -> tuple[int, int, int] | None:
    match = RUST_TAG_RE.fullmatch(ref_name)
    if match is None:
        return None
    return tuple(int(part) for part in match.groups())


def load_baseline(path: Path) -> dict[str, str]:
    metadata = json.loads(path.read_text(encoding="utf-8"))
    source_commit = metadata.get("source_commit", "")
    source_ref_name = metadata.get("source_ref_name") or source_commit
    source_ref_kind = normalize_kind(metadata.get("source_ref_kind", ""), source_ref_name)
    return {
        "source_commit": source_commit,
        "source_ref_name": source_ref_name,
        "source_ref_kind": source_ref_kind,
    }


def result(
    decision: str,
    reason: str,
    *,
    baseline: dict[str, str],
    target_ref: str,
    target_kind: str,
    target_sha: str,
    target_explicit: bool,
    mode: str,
) -> dict[str, Any]:
    return {
        "decision": decision,
        "reason": reason,
        "baseline_ref_name": baseline["source_ref_name"],
        "baseline_ref_kind": baseline["source_ref_kind"],
        "baseline_commit": baseline["source_commit"],
        "target_ref_name": target_ref,
        "target_ref_kind": target_kind,
        "target_commit": target_sha,
        "target_explicit": target_explicit,
        "mode": mode,
    }


def evaluate_policy(
    baseline: dict[str, str],
    *,
    target_ref: str,
    target_kind: str,
    target_sha: str,
    target_explicit: bool,
    mode: str,
    allow_downgrade: bool,
) -> dict[str, Any]:
    baseline_kind = normalize_kind(baseline.get("source_ref_kind", ""), baseline.get("source_ref_name", ""))
    target_kind = normalize_kind(target_kind, target_ref)
    normalized_baseline = {
        "source_commit": baseline.get("source_commit", ""),
        "source_ref_name": baseline.get("source_ref_name", "") or baseline.get("source_commit", ""),
        "source_ref_kind": baseline_kind,
    }

    if normalized_baseline["source_commit"] == target_sha:
        return result(
            "skip",
            "baseline already points at the selected upstream commit",
            baseline=normalized_baseline,
            target_ref=target_ref,
            target_kind=target_kind,
            target_sha=target_sha,
            target_explicit=target_explicit,
            mode=mode,
        )

    if baseline_kind not in VALID_KINDS:
        return result(
            "block",
            f"unsupported baseline source_ref_kind: {baseline_kind}",
            baseline=normalized_baseline,
            target_ref=target_ref,
            target_kind=target_kind,
            target_sha=target_sha,
            target_explicit=target_explicit,
            mode=mode,
        )
    if target_kind not in VALID_KINDS:
        return result(
            "block",
            f"unsupported target source_ref_kind: {target_kind}",
            baseline=normalized_baseline,
            target_ref=target_ref,
            target_kind=target_kind,
            target_sha=target_sha,
            target_explicit=target_explicit,
            mode=mode,
        )

    if target_kind == STABLE_TAG:
        target_version = parse_rust_tag(target_ref)
        if target_version is None:
            return result(
                "block",
                f"stable target is not a rust-vX.Y.Z tag: {target_ref}",
                baseline=normalized_baseline,
                target_ref=target_ref,
                target_kind=target_kind,
                target_sha=target_sha,
                target_explicit=target_explicit,
                mode=mode,
            )

        if baseline_kind == STABLE_TAG:
            baseline_version = parse_rust_tag(normalized_baseline["source_ref_name"])
            if baseline_version is None:
                return result(
                    "block",
                    f"stable baseline is not a rust-vX.Y.Z tag: {normalized_baseline['source_ref_name']}",
                    baseline=normalized_baseline,
                    target_ref=target_ref,
                    target_kind=target_kind,
                    target_sha=target_sha,
                    target_explicit=target_explicit,
                    mode=mode,
                )
            if target_version > baseline_version:
                return result(
                    "allow",
                    "stable tag moves forward by version",
                    baseline=normalized_baseline,
                    target_ref=target_ref,
                    target_kind=target_kind,
                    target_sha=target_sha,
                    target_explicit=target_explicit,
                    mode=mode,
                )
            if target_version == baseline_version:
                return result(
                    "block",
                    "stable tag name matches baseline but peeled commit changed",
                    baseline=normalized_baseline,
                    target_ref=target_ref,
                    target_kind=target_kind,
                    target_sha=target_sha,
                    target_explicit=target_explicit,
                    mode=mode,
                )
            if allow_downgrade and target_explicit:
                return result(
                    "allow",
                    "explicit stable tag downgrade requested",
                    baseline=normalized_baseline,
                    target_ref=target_ref,
                    target_kind=target_kind,
                    target_sha=target_sha,
                    target_explicit=target_explicit,
                    mode=mode,
                )
            return result(
                "block",
                "stable tag target is older than the current baseline tag",
                baseline=normalized_baseline,
                target_ref=target_ref,
                target_kind=target_kind,
                target_sha=target_sha,
                target_explicit=target_explicit,
                mode=mode,
            )

        if baseline_kind in {MANUAL_REF, MANUAL_COMMIT}:
            if target_explicit:
                return result(
                    "allow",
                    "explicit track switch from manual baseline to stable tag",
                    baseline=normalized_baseline,
                    target_ref=target_ref,
                    target_kind=target_kind,
                    target_sha=target_sha,
                    target_explicit=target_explicit,
                    mode=mode,
                )
            return result(
                "block",
                "current baseline is manual; automatic stable-tag drift detection would switch tracks",
                baseline=normalized_baseline,
                target_ref=target_ref,
                target_kind=target_kind,
                target_sha=target_sha,
                target_explicit=target_explicit,
                mode=mode,
            )

    if target_kind in {MANUAL_REF, MANUAL_COMMIT}:
        if target_explicit:
            return result(
                "allow",
                "explicit manual upstream target requested",
                baseline=normalized_baseline,
                target_ref=target_ref,
                target_kind=target_kind,
                target_sha=target_sha,
                target_explicit=target_explicit,
                mode=mode,
            )
        return result(
            "block",
            "manual upstream targets must be explicit",
            baseline=normalized_baseline,
            target_ref=target_ref,
            target_kind=target_kind,
            target_sha=target_sha,
            target_explicit=target_explicit,
            mode=mode,
        )

    return result(
        "block",
        "target policy did not match any allowed transition",
        baseline=normalized_baseline,
        target_ref=target_ref,
        target_kind=target_kind,
        target_sha=target_sha,
        target_explicit=target_explicit,
        mode=mode,
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", required=True, type=Path)
    parser.add_argument("--target-ref", required=True)
    parser.add_argument("--target-kind", required=True)
    parser.add_argument("--target-sha", required=True)
    parser.add_argument("--target-explicit", default="false", type=parse_bool)
    parser.add_argument("--mode", choices=("scheduled", "manual"), default="manual")
    parser.add_argument("--allow-downgrade", default="false", type=parse_bool)
    parser.add_argument("--json", action="store_true", help="print machine-readable decision JSON to stdout")
    args = parser.parse_args()

    decision = evaluate_policy(
        load_baseline(args.baseline),
        target_ref=args.target_ref,
        target_kind=args.target_kind,
        target_sha=args.target_sha,
        target_explicit=args.target_explicit,
        mode=args.mode,
        allow_downgrade=args.allow_downgrade,
    )
    if args.json:
        print(json.dumps(decision, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

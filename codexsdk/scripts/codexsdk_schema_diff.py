#!/usr/bin/env python3
"""Compare Codex app-server schema directories and write drift reports."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

import codexsdk_schema_utils as schema_utils


VALID_STATUSES = [
    "supported",
    "supported-generated",
    "deferred",
    "intentionally-unsupported",
]


def build_reports(
    *,
    baseline: Path,
    candidate: Path,
    reports: Path,
    source_commit: str,
    source_ref: str,
    source_ref_kind: str,
    codex_version: str,
    generator: str,
    generator_detail: str,
) -> tuple[dict[str, Any], dict[str, Any], str]:
    file_diff = schema_utils.schema_diff(baseline, candidate)
    method_diff = schema_utils.aggregate_method_diff(baseline, candidate)
    status = "clean"
    if (
        file_diff["added"]
        or file_diff["changed"]
        or file_diff["removed"]
        or any(value["added"] or value["removed"] for value in method_diff.values())
    ):
        status = "review-required"

    drift = {
        "status": status,
        "comparison_mode": "canonical-json",
        "target": {
            "source_repo": "https://github.com/openai/codex",
            "source_ref_name": source_ref,
            "source_ref_kind": source_ref_kind,
            "source_commit": source_commit,
            "codex_version": codex_version,
            "generator": generator,
            "generator_detail": generator_detail,
            "schema_bundle_sha256": schema_utils.schema_bundle_sha256(candidate),
            "canonical_json_note": "Schema comparisons use canonical JSON; object member ordering is irrelevant.",
        },
        "file_diff": file_diff,
        "method_diff": method_diff,
        "matrix_update_skeleton": "matrix_update_skeleton.json",
    }

    matrix = {
        "status": "empty" if status == "clean" else "review-required",
        "source": "drift_summary.json",
        "valid_statuses": VALID_STATUSES,
        "method_updates": [
            {"method": method, "source_schema": schema, "change": change, "status": "review-required"}
            for schema, diff in method_diff.items()
            for change, methods in (("added", diff["added"]), ("removed", diff["removed"]))
            for method in methods
        ],
        "type_updates": (
            [{"schema": path, "change": "added", "status": "review-required"} for path in file_diff["added"]]
            + [{"schema": path, "change": "changed", "status": "review-required"} for path in file_diff["changed"]]
            + [{"schema": path, "change": "removed", "status": "review-required"} for path in file_diff["removed"]]
        ),
        "field_updates": [],
    }

    summary = "\n".join(
        [
            "# Codex SDK Upstream Tracking",
            "",
            f"- status: `{status}`",
            "- source repo: `https://github.com/openai/codex`",
            f"- source ref: `{source_ref}`",
            f"- source ref kind: `{source_ref_kind}`",
            f"- source commit: `{source_commit}`",
            f"- codex version: `{codex_version}`",
            f"- generated schema: `{candidate}`",
            f"- drift summary: `{reports / 'drift_summary.json'}`",
            f"- matrix update skeleton: `{reports / 'matrix_update_skeleton.json'}`",
            "",
            "Review the generated reports before updating the checked-in baseline.",
            "",
        ]
    )
    return drift, matrix, summary


def write_reports(reports: Path, drift: dict[str, Any], matrix: dict[str, Any], summary: str) -> Path:
    reports.mkdir(parents=True, exist_ok=True)
    (reports / "drift_summary.json").write_text(json.dumps(drift, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    (reports / "matrix_update_skeleton.json").write_text(
        json.dumps(matrix, indent=2, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )
    summary_path = reports / "SUMMARY.md"
    summary_path.write_text(summary, encoding="utf-8")
    return summary_path


def diff_count_summary(drift: dict[str, Any]) -> str:
    file_diff = drift["file_diff"]
    method_deltas = sum(
        len(diff["added"]) + len(diff["removed"])
        for diff in drift["method_diff"].values()
    )
    return (
        f"status={drift['status']} "
        f"files_added={len(file_diff['added'])} "
        f"files_changed={len(file_diff['changed'])} "
        f"files_removed={len(file_diff['removed'])} "
        f"method_deltas={method_deltas}"
    )


def existing_dir(value: str) -> Path:
    path = Path(value)
    if not path.is_dir():
        raise argparse.ArgumentTypeError(f"directory does not exist: {value}")
    return path


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", required=True, type=existing_dir, help="checked-in SDK schema baseline directory")
    parser.add_argument("--candidate", required=True, type=existing_dir, help="candidate schema directory to compare")
    parser.add_argument("--reports", type=Path, help="output directory for drift reports")
    parser.add_argument("--source-commit", required=True, help="resolved upstream source commit")
    parser.add_argument("--source-ref", required=True, help="upstream tag/ref name used for provenance")
    parser.add_argument("--source-ref-kind", required=True, help="upstream target kind")
    parser.add_argument("--codex-version", required=True, help="Codex generator version text")
    parser.add_argument("--generator", required=True, help="generator mode or provenance label")
    parser.add_argument("--generator-detail", required=True, help="generator command or candidate provenance")
    parser.add_argument("--json", action="store_true", help="print drift summary JSON to stdout")
    parser.add_argument("--verbose", action="store_true", help="print human-readable report paths and counts to stderr")
    args = parser.parse_args()

    if args.reports is None and not args.json:
        parser.error("at least one of --reports or --json is required")

    reports = args.reports or Path(".")
    drift, matrix, summary = build_reports(
        baseline=args.baseline,
        candidate=args.candidate,
        reports=reports,
        source_commit=args.source_commit,
        source_ref=args.source_ref,
        source_ref_kind=args.source_ref_kind,
        codex_version=args.codex_version,
        generator=args.generator,
        generator_detail=args.generator_detail,
    )
    summary_path = None
    if args.reports is not None:
        summary_path = write_reports(args.reports, drift, matrix, summary)
    if args.json:
        print(json.dumps(drift, sort_keys=True))
    if args.verbose:
        print(diff_count_summary(drift), file=sys.stderr)
        if summary_path is not None:
            print(f"drift summary: {args.reports / 'drift_summary.json'}", file=sys.stderr)
            print(f"matrix update skeleton: {args.reports / 'matrix_update_skeleton.json'}", file=sys.stderr)
            print(f"summary: {summary_path}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

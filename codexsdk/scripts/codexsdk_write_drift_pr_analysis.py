#!/usr/bin/env python3
"""Write a PR-ready drift analysis artifact from drift_summary.json."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
from typing import Any


def short_list(items: list[str], limit: int = 20) -> str:
    if not items:
        return "_none_"
    lines = [f"- `{item}`" for item in items[:limit]]
    if len(items) > limit:
        lines.append(f"- ...and {len(items) - limit} more")
    return "\n".join(lines)


def method_diff_markdown(method_diff: dict[str, Any]) -> str:
    method_lines: list[str] = []
    for schema, diff in method_diff.items():
        added = diff.get("added", [])
        removed = diff.get("removed", [])
        if added or removed:
            method_lines.extend(
                [
                    f"### {schema}",
                    "",
                    "Added:",
                    short_list(added),
                    "",
                    "Removed:",
                    short_list(removed),
                    "",
                ]
            )
    if not method_lines:
        return "_No aggregate method additions or removals._"
    return "\n".join(method_lines).rstrip()


def render_analysis(
    *,
    baseline_sha: str,
    drift: dict[str, Any],
    drift_sha: str,
    run_url: str,
    upstream_ref: str,
    upstream_ref_kind: str,
    upstream_sha: str,
) -> str:
    file_diff = drift["file_diff"]
    lines = [
        f"Status: `{drift['status']}`",
        "",
        f"- Upstream ref: `{upstream_ref}`",
        f"- Upstream ref kind: `{upstream_ref_kind}`",
        f"- Upstream commit: `{upstream_sha}`",
        f"- Current baseline commit: `{baseline_sha}`",
        f"- Drift fingerprint: `{drift_sha}`",
        f"- Workflow run: {run_url}",
        "",
        "### Schema File Diff",
        "",
        f"Added ({len(file_diff['added'])}):",
        short_list(file_diff["added"]),
        "",
        f"Changed ({len(file_diff['changed'])}):",
        short_list(file_diff["changed"]),
        "",
        f"Removed ({len(file_diff['removed'])}):",
        short_list(file_diff["removed"]),
        "",
        "### Method Diff",
        "",
        method_diff_markdown(drift["method_diff"]),
        "",
    ]
    return "\n".join(lines)


def write_github_output(status: str, drift_sha: str) -> None:
    output_path = os.environ.get("GITHUB_OUTPUT")
    if not output_path:
        return
    with open(output_path, "a", encoding="utf-8") as output:
        output.write(f"status={status}\n")
        output.write(f"drift_sha={drift_sha}\n")


def write_artifacts(
    *,
    artifact_dir: Path,
    baseline_sha: str,
    run_url: str,
    upstream_ref: str,
    upstream_ref_kind: str,
    upstream_sha: str,
) -> dict[str, str]:
    reports = artifact_dir / "reports"
    drift_path = reports / "drift_summary.json"
    drift = json.loads(drift_path.read_text(encoding="utf-8"))
    drift_sha = hashlib.sha256(drift_path.read_bytes()).hexdigest()
    analysis = render_analysis(
        baseline_sha=baseline_sha,
        drift=drift,
        drift_sha=drift_sha,
        run_url=run_url,
        upstream_ref=upstream_ref,
        upstream_ref_kind=upstream_ref_kind,
        upstream_sha=upstream_sha,
    )

    artifact_dir.mkdir(parents=True, exist_ok=True)
    (artifact_dir / "drift-analysis.md").write_text(analysis, encoding="utf-8")
    write_github_output(drift["status"], drift_sha)

    return {
        "drift_sha": drift_sha,
        "path": str(artifact_dir / "drift-analysis.md"),
        "status": drift["status"],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifact-dir", required=True, type=Path, help="drift artifact directory containing reports/")
    parser.add_argument("--baseline-sha", required=True, help="current checked-in upstream baseline commit")
    parser.add_argument("--run-url", required=True, help="GitHub Actions run URL")
    parser.add_argument("--upstream-ref", required=True, help="selected upstream ref")
    parser.add_argument("--upstream-ref-kind", required=True, help="selected upstream ref kind")
    parser.add_argument("--upstream-sha", required=True, help="selected upstream commit SHA")
    parser.add_argument("--json", action="store_true", help="print a machine-readable summary")
    args = parser.parse_args()

    payload = write_artifacts(
        artifact_dir=args.artifact_dir,
        baseline_sha=args.baseline_sha,
        run_url=args.run_url,
        upstream_ref=args.upstream_ref,
        upstream_ref_kind=args.upstream_ref_kind,
        upstream_sha=args.upstream_sha,
    )
    if args.json:
        print(json.dumps(payload, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

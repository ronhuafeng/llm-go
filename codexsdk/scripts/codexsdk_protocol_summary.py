#!/usr/bin/env python3
"""Render an observation-only protocol proof summary.

This program projects already-observed workflow/job/proof facts. It does not
decide workflow success and does not authorize publication.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


OWNER_JOBS = (
    ("generated", "Generated reproducibility"),
    ("owner_local", "Owner-local tests"),
    ("schema_state", "Candidate schema state"),
    ("script_tests", "Remaining script tests"),
)

UNOBSERVED = "unobserved"


def load_json(path: str) -> Any:
    if not path:
        return None
    raw = Path(path).read_text(encoding="utf-8")
    return json.loads(raw)


def job_observation(jobs: list[dict[str, Any]], suffix: str) -> str:
    matches = []
    for job in jobs:
        name = str(job.get("name") or "")
        if name == suffix or name.endswith(" / " + suffix):
            matches.append(job)
    if not matches:
        return UNOBSERVED
    conclusions = []
    for job in matches:
        conclusion = str(job.get("conclusion") or job.get("status") or "").strip()
        if conclusion in {"", "null"}:
            conclusions.append(UNOBSERVED)
        elif conclusion == "skipped":
            conclusions.append("skipped")
        else:
            conclusions.append(conclusion)
    unique = sorted(set(conclusions))
    if len(unique) == 1:
        return unique[0]
    return "conflict:" + ",".join(unique)


def json_presence(payload: dict[str, Any] | None, key: str) -> str:
    if not payload or key not in payload or payload[key] is None:
        return UNOBSERVED
    value = payload[key]
    if isinstance(value, bool):
        return "true" if value else "false"
    text = str(value).strip()
    return text if text else UNOBSERVED


def publication_observation(*, validation_only: bool, publish_result: str) -> str:
    result = publish_result.strip() or UNOBSERVED
    if validation_only:
        if result in {UNOBSERVED, "skipped"}:
            return "skipped: validation-only policy"
        return f"{result} (validation-only policy requested skip)"
    if result == "skipped":
        return "skipped: proof did not authorize publication"
    return result


def render(
    *,
    jobs: list[dict[str, Any]],
    generated_proof: dict[str, Any] | None,
    repository_sha: str,
    target_ref: str,
    target_kind: str,
    target_sha: str,
    outcome: str,
    validation_only: bool,
    publish_result: str,
    source_run_id: str,
    source_run_attempt: str,
) -> str:
    lines = [
        "# Protocol proof observation",
        "",
        "This summary is a projection of already-observed facts.",
        "It does not decide success and does not authorize publication.",
        "",
        "## Identity",
        "",
        f"- repository commit: `{repository_sha or UNOBSERVED}`",
        f"- upstream ref: `{target_ref or UNOBSERVED}`",
        f"- upstream ref kind: `{target_kind or UNOBSERVED}`",
        f"- upstream commit: `{target_sha or UNOBSERVED}`",
        f"- mechanical outcome: `{outcome or UNOBSERVED}`",
    ]
    if source_run_id:
        lines.append(f"- source failed run: `{source_run_id}`")
        lines.append(f"- source failed run attempt: `{source_run_attempt or UNOBSERVED}`")
        lines.append("- this repair continuation is a separate operation from the source failed run")
    lines.extend(
        [
            "",
            "## Generated proof artifact",
            "",
            f"- repository_commit: `{json_presence(generated_proof, 'repository_commit')}`",
            f"- repository_tree: `{json_presence(generated_proof, 'repository_tree')}`",
            f"- worktree_overlay: `{json_presence(generated_proof, 'worktree_overlay')}`",
            f"- upstream_ref: `{json_presence(generated_proof, 'upstream_ref')}`",
            f"- upstream_ref_kind: `{json_presence(generated_proof, 'upstream_ref_kind')}`",
            f"- upstream_commit: `{json_presence(generated_proof, 'upstream_commit')}`",
            f"- generated_artifacts_reproducible: `{json_presence(generated_proof, 'generated_artifacts_reproducible')}`",
            f"- baseline_path_leak: `{json_presence(generated_proof, 'baseline_path_leak')}`",
            "",
            "## Proof owners",
            "",
        ]
    )
    for key, suffix in OWNER_JOBS:
        lines.append(f"- {key}: `{job_observation(jobs, suffix)}`")
    lines.extend(
        [
            "",
            "## Publication",
            "",
            f"- `{publication_observation(validation_only=validation_only, publish_result=publish_result)}`",
            "",
        ]
    )
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--jobs-json", required=True)
    parser.add_argument("--generated-proof", default="")
    parser.add_argument("--repository-sha", default="")
    parser.add_argument("--target-ref", default="")
    parser.add_argument("--target-kind", default="")
    parser.add_argument("--target-sha", default="")
    parser.add_argument("--outcome", default="")
    parser.add_argument("--validation-only", choices=("true", "false"), required=True)
    parser.add_argument("--publish-result", default="")
    parser.add_argument("--source-run-id", default="")
    parser.add_argument("--source-run-attempt", default="")
    parser.add_argument("--out", default="")
    args = parser.parse_args()

    payload = load_json(args.jobs_json)
    if isinstance(payload, dict):
        jobs = payload.get("jobs") or []
    elif isinstance(payload, list):
        jobs = payload
    else:
        raise SystemExit("jobs JSON must be a list or an object with jobs")
    generated_proof = None
    if args.generated_proof:
        generated_path = Path(args.generated_proof)
        if generated_path.is_file():
            generated_proof = load_json(str(generated_path))
            if not isinstance(generated_proof, dict):
                generated_proof = None

    text = render(
        jobs=jobs,
        generated_proof=generated_proof,
        repository_sha=args.repository_sha,
        target_ref=args.target_ref,
        target_kind=args.target_kind,
        target_sha=args.target_sha,
        outcome=args.outcome,
        validation_only=args.validation_only == "true",
        publish_result=args.publish_result,
        source_run_id=args.source_run_id,
        source_run_attempt=args.source_run_attempt,
    )
    if args.out:
        Path(args.out).write_text(text, encoding="utf-8")
    else:
        print(text, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

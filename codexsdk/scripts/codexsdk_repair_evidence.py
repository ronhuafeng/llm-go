#!/usr/bin/env python3
"""Pack and validate protocol-sync failure evidence for a separate repair run.

The normal sync workflow writes exact observed artifacts. The repair workflow
loads a failed run by ID and refuses to continue unless identity and provenance
match. This module does not decide publication mode.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any


SYNC_WORKFLOW_PATH = ".github/workflows/codexsdk-upstream-protocol-sync.yml"
EVIDENCE_NAME = "evidence.json"


class EvidenceError(SystemExit):
    pass


def load_json(path: Path) -> dict[str, Any]:
    raw = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(raw, dict):
        raise EvidenceError(f"{path} is not a JSON object")
    return raw


def write_json(path: Path, payload: dict[str, Any]) -> dict[str, Any]:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return payload


def build_evidence(
    *,
    repository: str,
    workflow_path: str,
    run_id: str,
    repository_sha: str,
    target_ref: str,
    target_kind: str,
    target_sha: str,
    outcome: str,
    applied: bool,
    candidate_present: bool,
    worktree_present: bool,
) -> dict[str, Any]:
    return {
        "repository": repository,
        "workflow_path": workflow_path,
        "run_id": str(run_id),
        "repository_sha": repository_sha,
        "target_ref": target_ref,
        "target_kind": target_kind,
        "target_sha": target_sha,
        "outcome": outcome,
        "applied": bool(applied),
        "candidate_present": bool(candidate_present),
        "worktree_present": bool(worktree_present),
    }


def validate_failed_run(
    *,
    run: dict[str, Any],
    evidence: dict[str, Any],
    repository: str,
    workflow_path: str = SYNC_WORKFLOW_PATH,
    artifacts: list[dict[str, Any]] | None = None,
) -> dict[str, Any]:
    if str(run.get("conclusion") or "") != "failure":
        raise EvidenceError(
            f"failed_run_id conclusion={run.get('conclusion')!r}, want failure"
        )
    head_repo = ""
    head = run.get("head_repository")
    if isinstance(head, dict):
        head_repo = str(head.get("full_name") or "")
    if head_repo and head_repo != repository:
        raise EvidenceError(f"failed run head repository {head_repo!r} != {repository!r}")
    path = str(run.get("path") or "")
    if path != workflow_path:
        raise EvidenceError(f"failed run workflow path {path!r} != {workflow_path!r}")
    run_sha = str(run.get("head_sha") or "")
    evidence_sha = str(evidence.get("repository_sha") or "")
    if not evidence_sha or run_sha != evidence_sha:
        raise EvidenceError(
            f"failed run head_sha={run_sha!r} does not match evidence repository_sha={evidence_sha!r}"
        )
    if str(evidence.get("repository") or "") != repository:
        raise EvidenceError(
            f"evidence repository {evidence.get('repository')!r} != {repository!r}"
        )
    if str(evidence.get("workflow_path") or "") != workflow_path:
        raise EvidenceError(
            f"evidence workflow_path {evidence.get('workflow_path')!r} != {workflow_path!r}"
        )
    run_id = str(run.get("id") or "")
    if str(evidence.get("run_id") or "") != run_id:
        raise EvidenceError(
            f"evidence run_id {evidence.get('run_id')!r} != failed run {run_id!r}"
        )
    for key in ("target_ref", "target_sha", "target_kind", "repository_sha"):
        if not str(evidence.get(key) or ""):
            raise EvidenceError(f"evidence missing {key}")
    if artifacts is not None:
        for artifact in artifacts:
            owner = artifact.get("workflow_run")
            owner_id = ""
            if isinstance(owner, dict):
                owner_id = str(owner.get("id") or "")
            if owner_id and owner_id != run_id:
                raise EvidenceError(
                    f"artifact {artifact.get('name')!r} belongs to run {owner_id}, want {run_id}"
                )
    return evidence


def pack_directory(
    *,
    out_dir: Path,
    repository: str,
    run_id: str,
    repository_sha: str,
    target_ref: str,
    target_kind: str,
    target_sha: str,
    outcome: str,
    applied: bool,
    candidate: str,
    repo_root: Path,
) -> dict[str, Any]:
    out_dir.mkdir(parents=True, exist_ok=True)
    worktree_present = False
    candidate_present = False
    status = subprocess.run(
        ["git", "-C", str(repo_root), "status", "--porcelain", "--untracked-files=all"],
        check=True,
        stdout=subprocess.PIPE,
        text=True,
    )
    if status.stdout.strip():
        subprocess.run(["git", "-C", str(repo_root), "add", "-A"], check=True)
        patch = subprocess.run(
            ["git", "-C", str(repo_root), "diff", "--cached", "--binary"],
            check=True,
            stdout=subprocess.PIPE,
        )
        (out_dir / "protocol-worktree.patch").write_bytes(patch.stdout)
        subprocess.run(["git", "-C", str(repo_root), "reset", "-q"], check=True)
        worktree_present = True
    if candidate:
        source = Path(candidate)
        if source.is_dir():
            dest = out_dir / "schema"
            if dest.exists():
                shutil.rmtree(dest)
            shutil.copytree(source, dest)
            candidate_present = True
    evidence = build_evidence(
        repository=repository,
        workflow_path=SYNC_WORKFLOW_PATH,
        run_id=run_id,
        repository_sha=repository_sha,
        target_ref=target_ref,
        target_kind=target_kind,
        target_sha=target_sha,
        outcome=outcome,
        applied=applied,
        candidate_present=candidate_present,
        worktree_present=worktree_present,
    )
    write_json(out_dir / EVIDENCE_NAME, evidence)
    return evidence


def _cmd_pack(args: argparse.Namespace) -> int:
    evidence = pack_directory(
        out_dir=args.out,
        repository=args.repository,
        run_id=args.run_id,
        repository_sha=args.repository_sha,
        target_ref=args.target_ref,
        target_kind=args.target_kind,
        target_sha=args.target_sha,
        outcome=args.outcome,
        applied=args.applied,
        candidate=args.candidate,
        repo_root=args.repo_root,
    )
    output = os.environ.get("GITHUB_OUTPUT")
    if output:
        with Path(output).open("a", encoding="utf-8") as handle:
            handle.write(f"restore_candidate={'true' if evidence['candidate_present'] else 'false'}\n")
            handle.write(f"restore_worktree={'true' if evidence['worktree_present'] else 'false'}\n")
    print(json.dumps(evidence, indent=2, sort_keys=True))
    return 0


def _cmd_validate(args: argparse.Namespace) -> int:
    run = load_json(args.run_json)
    evidence = load_json(args.evidence)
    artifacts = None
    if args.artifacts_json:
        raw = json.loads(args.artifacts_json.read_text(encoding="utf-8"))
        if isinstance(raw, dict) and isinstance(raw.get("artifacts"), list):
            artifacts = raw["artifacts"]
        elif isinstance(raw, list):
            artifacts = raw
        else:
            raise EvidenceError("artifacts JSON must be a list or {artifacts: [...]} ")
    validate_failed_run(
        run=run,
        evidence=evidence,
        repository=args.repository,
        workflow_path=args.workflow_path,
        artifacts=artifacts,
    )
    print(json.dumps(evidence, indent=2, sort_keys=True))
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)

    pack = sub.add_parser("pack", help="write observed sync evidence into a directory")
    pack.add_argument("--out", required=True, type=Path)
    pack.add_argument("--repo-root", required=True, type=Path)
    pack.add_argument("--repository", required=True)
    pack.add_argument("--run-id", required=True)
    pack.add_argument("--repository-sha", required=True)
    pack.add_argument("--target-ref", required=True)
    pack.add_argument("--target-kind", required=True)
    pack.add_argument("--target-sha", required=True)
    pack.add_argument("--outcome", required=True)
    pack.add_argument("--applied", action="store_true")
    pack.add_argument("--candidate", default="")
    pack.set_defaults(func=_cmd_pack)

    validate = sub.add_parser("validate", help="fail closed unless failed-run identity matches evidence")
    validate.add_argument("--run-json", required=True, type=Path)
    validate.add_argument("--evidence", required=True, type=Path)
    validate.add_argument("--repository", required=True)
    validate.add_argument("--workflow-path", default=SYNC_WORKFLOW_PATH)
    validate.add_argument("--artifacts-json", type=Path)
    validate.set_defaults(func=_cmd_validate)

    args = parser.parse_args()
    return int(args.func(args))


if __name__ == "__main__":
    raise SystemExit(main())

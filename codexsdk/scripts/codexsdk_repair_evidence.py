#!/usr/bin/env python3
"""Pack observed protocol-sync evidence and admit a repair continuation.

The normal sync workflow writes only facts it actually observed. The repair
workflow classifies that source run before Codex may run. This module does not
decide publication mode or generated-Go correctness.
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
PLACEHOLDERS = frozenset({"", "unknown", "not-run", "missing-but-assumed"})
PROOF_OWNER_JOB_NAMES = {
    "Protocol proof / Generated reproducibility": "generated",
    "Protocol proof / Owner-local tests": "owner-local",
    "Protocol proof / Candidate schema state": "schema-state",
    "Protocol proof / Remaining script tests": "script-tests",
    "Generated reproducibility": "generated",
    "Owner-local tests": "owner-local",
    "Candidate schema state": "schema-state",
    "Remaining script tests": "script-tests",
}
PROOF_OWNER_ORDER = ("generated", "owner-local", "schema-state", "script-tests")
CONTROL_FILES = (
    "action-inputs.json",
    "policy.json",
    "mechanical-outcome.json",
    "escalation.json",
    "mechanical-changes.json",
)
NON_REPAIRABLE_OUTCOMES = frozenset(
    {"blocked", "current", "comparison", "comparison_dirty"}
)


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


def observed_text(value: object) -> str | None:
    if value is None:
        return None
    text = str(value).strip()
    if not text or text.lower() in PLACEHOLDERS:
        return None
    return text


def load_list(path: Path, key: str) -> list[dict[str, Any]]:
    raw = json.loads(path.read_text(encoding="utf-8"))
    if isinstance(raw, list):
        return [item for item in raw if isinstance(item, dict)]
    if isinstance(raw, dict) and isinstance(raw.get(key), list):
        return [item for item in raw[key] if isinstance(item, dict)]
    raise EvidenceError(f"{path} must be a list or {{{key}: [...]}}")


def proof_owner_for_job(name: str) -> str | None:
    return PROOF_OWNER_JOB_NAMES.get(name.strip())


def proof_outcomes_from_jobs(jobs: list[dict[str, Any]]) -> dict[str, str]:
    observed: dict[str, str] = {}
    for job in jobs:
        owner = proof_owner_for_job(str(job.get("name") or ""))
        if owner is None:
            continue
        conclusion = str(job.get("conclusion") or "")
        if conclusion not in {"success", "failure"}:
            continue
        observed[owner] = conclusion
    return {owner: observed[owner] for owner in PROOF_OWNER_ORDER if owner in observed}


def failed_jobs_from_jobs(jobs: list[dict[str, Any]]) -> list[dict[str, str]]:
    failed: list[dict[str, str]] = []
    for job in jobs:
        owner = proof_owner_for_job(str(job.get("name") or ""))
        if owner is None:
            continue
        if str(job.get("conclusion") or "") != "failure":
            continue
        failed.append(
            {
                "owner": owner,
                "name": str(job.get("name") or ""),
                "job_id": str(job.get("id") or ""),
            }
        )
    return failed


def build_evidence(
    *,
    repository: str,
    workflow_path: str,
    run_id: str,
    repository_sha: str = "",
    target_ref: str = "",
    target_kind: str = "",
    target_sha: str = "",
    outcome: str = "",
    applied: bool = False,
    candidate_present: bool = False,
    worktree_present: bool = False,
    head_branch: str = "",
    event: str = "",
    validation_only: bool | None = None,
    control_files: list[str] | None = None,
    report_files: list[str] | None = None,
    mechanical_evidence_present: bool = False,
) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "repository": repository,
        "workflow_path": workflow_path,
        "run_id": str(run_id),
        "applied": bool(applied),
        "candidate_present": bool(candidate_present),
        "worktree_present": bool(worktree_present),
        "mechanical_evidence_present": bool(mechanical_evidence_present),
    }
    for key, value in (
        ("repository_sha", repository_sha),
        ("target_ref", target_ref),
        ("target_kind", target_kind),
        ("target_sha", target_sha),
        ("outcome", outcome),
        ("head_branch", head_branch),
        ("event", event),
    ):
        observed = observed_text(value)
        if observed is not None:
            payload[key] = observed
    if validation_only is not None:
        payload["validation_only"] = bool(validation_only)
    if control_files:
        payload["control_files"] = list(control_files)
    if report_files:
        payload["report_files"] = list(report_files)
    return payload


def copy_existing_file(source: Path, dest: Path) -> bool:
    if not source.is_file():
        return False
    dest.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, dest)
    return True


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
    head_branch: str = "",
    event: str = "",
    validation_only: bool | None = None,
    module_root: Path | None = None,
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
    control_files: list[str] = []
    cache = (module_root or (repo_root / "codexsdk")) / ".cache" / "codexsdk-sync"
    control_dir = out_dir / "control"
    for name in CONTROL_FILES:
        if copy_existing_file(cache / name, control_dir / name):
            control_files.append(name)
    report_files: list[str] = []
    if candidate:
        reports = Path(candidate).resolve().parent / "reports"
        if reports.is_dir():
            dest_reports = out_dir / "reports"
            for path in sorted(reports.rglob("*")):
                if not path.is_file():
                    continue
                rel = path.relative_to(reports)
                if copy_existing_file(path, dest_reports / rel):
                    report_files.append(rel.as_posix())
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
        head_branch=head_branch,
        event=event,
        validation_only=validation_only,
        control_files=control_files,
        report_files=report_files,
        mechanical_evidence_present="escalation.json" in control_files,
    )
    write_json(out_dir / EVIDENCE_NAME, evidence)
    return evidence


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
    evidence_sha = observed_text(evidence.get("repository_sha"))
    if evidence_sha is None or run_sha != evidence_sha:
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
        if observed_text(evidence.get(key)) is None:
            raise EvidenceError(f"evidence missing observed {key}")
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


def admit(
    *,
    run: dict[str, Any],
    evidence: dict[str, Any],
    repository: str,
    default_branch: str,
    jobs: list[dict[str, Any]] | None = None,
    artifacts: list[dict[str, Any]] | None = None,
    workflow_path: str = SYNC_WORKFLOW_PATH,
) -> dict[str, Any]:
    validate_failed_run(
        run=run,
        evidence=evidence,
        repository=repository,
        workflow_path=workflow_path,
        artifacts=artifacts,
    )
    head_branch = observed_text(evidence.get("head_branch")) or observed_text(run.get("head_branch"))
    if head_branch != observed_text(default_branch):
        raise EvidenceError(
            f"failed run branch {head_branch!r} is not default branch {default_branch!r}"
        )
    if evidence.get("validation_only") is True:
        raise EvidenceError("validation-only runs are not repairable")
    outcome = observed_text(evidence.get("outcome"))
    if outcome is None:
        raise EvidenceError("evidence missing observed outcome")
    if outcome in NON_REPAIRABLE_OUTCOMES:
        raise EvidenceError(f"outcome {outcome} is not repairable")
    proof_outcomes = proof_outcomes_from_jobs(jobs or [])
    failed_owners = [owner for owner in PROOF_OWNER_ORDER if proof_outcomes.get(owner) == "failure"]
    failed_jobs = failed_jobs_from_jobs(jobs or [])
    if outcome == "escalate":
        if not evidence.get("candidate_present"):
            raise EvidenceError("mechanical repair requires an observed candidate")
        if not evidence.get("mechanical_evidence_present"):
            raise EvidenceError("mechanical repair requires observed escalation evidence")
        failure_class = "mechanical"
    elif outcome == "applied":
        if not evidence.get("candidate_present"):
            raise EvidenceError("protocol-proof repair requires an observed candidate")
        if not evidence.get("worktree_present"):
            raise EvidenceError("protocol-proof repair requires an observed applied worktree")
        if not failed_owners:
            raise EvidenceError(
                "applied run with no failed protocol-proof owner is not repairable"
            )
        failure_class = "protocol-proof"
    else:
        raise EvidenceError(f"outcome {outcome!r} is not repairable")
    admission = {
        "failure_class": failure_class,
        "failed_proof_owners": failed_owners,
        "failed_jobs": failed_jobs,
        "proof_outcomes": proof_outcomes,
        "repository": repository,
        "workflow_path": workflow_path,
        "run_id": str(evidence.get("run_id") or ""),
        "repository_sha": evidence.get("repository_sha"),
        "target_ref": evidence.get("target_ref"),
        "target_kind": evidence.get("target_kind"),
        "target_sha": evidence.get("target_sha"),
        "outcome": outcome,
        "head_branch": head_branch,
        "candidate_present": bool(evidence.get("candidate_present")),
        "worktree_present": bool(evidence.get("worktree_present")),
    }
    return admission


def write_github_outputs(values: dict[str, str]) -> None:
    output = os.environ.get("GITHUB_OUTPUT")
    if not output:
        return
    with Path(output).open("a", encoding="utf-8") as handle:
        for key, value in values.items():
            handle.write(f"{key}={value}\n")


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
        head_branch=args.head_branch,
        event=args.event,
        validation_only=True if args.validation_only else None,
        module_root=args.module_root,
    )
    write_github_outputs(
        {
            "restore_candidate": "true" if evidence["candidate_present"] else "false",
            "restore_worktree": "true" if evidence["worktree_present"] else "false",
        }
    )
    print(json.dumps(evidence, indent=2, sort_keys=True))
    return 0


def _cmd_admit(args: argparse.Namespace) -> int:
    run = load_json(args.run_json)
    evidence = load_json(args.evidence)
    jobs = load_list(args.jobs_json, "jobs") if args.jobs_json else []
    artifacts = load_list(args.artifacts_json, "artifacts") if args.artifacts_json else None
    admission = admit(
        run=run,
        evidence=evidence,
        repository=args.repository,
        default_branch=args.default_branch,
        jobs=jobs,
        artifacts=artifacts,
        workflow_path=args.workflow_path,
    )
    write_json(args.out, admission)
    write_github_outputs(
        {
            "repository_sha": str(admission.get("repository_sha") or ""),
            "target_ref": str(admission.get("target_ref") or ""),
            "target_kind": str(admission.get("target_kind") or ""),
            "target_sha": str(admission.get("target_sha") or ""),
            "failure_class": str(admission.get("failure_class") or ""),
            "candidate_present": "true" if admission.get("candidate_present") else "false",
            "worktree_present": "true" if admission.get("worktree_present") else "false",
        }
    )
    print(json.dumps(admission, indent=2, sort_keys=True))
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)

    pack = sub.add_parser("pack", help="write observed sync evidence into a directory")
    pack.add_argument("--out", required=True, type=Path)
    pack.add_argument("--repo-root", required=True, type=Path)
    pack.add_argument("--module-root", type=Path)
    pack.add_argument("--repository", required=True)
    pack.add_argument("--run-id", required=True)
    pack.add_argument("--repository-sha", default="")
    pack.add_argument("--target-ref", default="")
    pack.add_argument("--target-kind", default="")
    pack.add_argument("--target-sha", default="")
    pack.add_argument("--outcome", default="")
    pack.add_argument("--head-branch", default="")
    pack.add_argument("--event", default="")
    pack.add_argument("--applied", action="store_true")
    pack.add_argument("--validation-only", action="store_true")
    pack.add_argument("--candidate", default="")
    pack.set_defaults(func=_cmd_pack)

    admit_cmd = sub.add_parser("admit", help="admit only explicitly repairable failed sync runs")
    admit_cmd.add_argument("--run-json", required=True, type=Path)
    admit_cmd.add_argument("--jobs-json", type=Path)
    admit_cmd.add_argument("--artifacts-json", type=Path)
    admit_cmd.add_argument("--evidence", required=True, type=Path)
    admit_cmd.add_argument("--repository", required=True)
    admit_cmd.add_argument("--default-branch", required=True)
    admit_cmd.add_argument("--workflow-path", default=SYNC_WORKFLOW_PATH)
    admit_cmd.add_argument("--out", required=True, type=Path)
    admit_cmd.set_defaults(func=_cmd_admit)

    args = parser.parse_args()
    return int(args.func(args))


if __name__ == "__main__":
    raise SystemExit(main())

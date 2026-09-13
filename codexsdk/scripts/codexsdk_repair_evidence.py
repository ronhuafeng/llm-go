#!/usr/bin/env python3
"""Pack observed protocol-sync evidence and admit a repair continuation.

GitHub run metadata owns source-run identity, including the exact run attempt.
Uploaded evidence may corroborate those facts, never override them. Protocol-proof
repair requires failure of the semantic owning step, not merely its job.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any


SYNC_WORKFLOW_PATH = ".github/workflows/codexsdk-upstream-protocol-sync.yml"
EVIDENCE_NAME = "evidence.json"
PLACEHOLDERS = frozenset({"", "unknown", "not-run", "missing-but-assumed"})
FULL_SHA = re.compile(r"^[0-9a-f]{40}$")
ALLOWED_EVENTS = frozenset({"schedule", "workflow_dispatch"})
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
SEMANTIC_STEP_NAMES = {
    "generated": "Prove generated artifacts",
    "owner-local": "Owner-local codexsdk tests",
    "schema-state": "Candidate schema state",
    "script-tests": "Remaining script tests",
}
PROOF_OWNER_ORDER = ("generated", "owner-local", "schema-state", "script-tests")
ARTIFACT_KINDS = (
    "protocol-sync-evidence",
    "protocol-candidate",
    "protocol-worktree",
    "generated-proof",
)
CONTROL_FILES = (
    "action-inputs.json",
    "policy.json",
    "mechanical-outcome.json",
    "escalation.json",
    "mechanical-changes.json",
)
CANDIDATE_DIRS = ("schema", "stable-schema")
CANDIDATE_FILES = ("common.rs", "common.rs.source_sha")
PRODUCT_PREFIXES = ("codexsdk/",)
PRODUCT_EXCLUDED_PREFIXES = ("codexsdk/.cache/", "codexsdk/.agents/")
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


def require_full_sha(value: object, *, field: str) -> str:
    text = observed_text(value)
    if text is None or not FULL_SHA.fullmatch(text):
        raise EvidenceError(f"{field} {value!r} is not a full 40-hex git sha")
    return text


def load_list(path: Path, key: str) -> list[dict[str, Any]]:
    raw = json.loads(path.read_text(encoding="utf-8"))
    if isinstance(raw, list):
        return [item for item in raw if isinstance(item, dict)]
    if isinstance(raw, dict) and isinstance(raw.get(key), list):
        return [item for item in raw[key] if isinstance(item, dict)]
    raise EvidenceError(f"{path} must be a list or {{{key}: [...]}}")


def artifact_name(kind: str, attempt: str) -> str:
    return f"{kind}-attempt-{attempt}"


def parse_artifact_attempt(name: str) -> tuple[str, str] | None:
    for kind in ARTIFACT_KINDS:
        prefix = f"{kind}-attempt-"
        if name.startswith(prefix):
            attempt = name[len(prefix) :]
            if attempt.isdigit():
                return kind, attempt
    return None


def proof_owner_for_job(name: str) -> str | None:
    return PROOF_OWNER_JOB_NAMES.get(name.strip())


def semantic_step_conclusion(job: dict[str, Any], owner: str) -> str | None:
    want = SEMANTIC_STEP_NAMES[owner]
    for step in job.get("steps") or []:
        if not isinstance(step, dict):
            continue
        if str(step.get("name") or "") != want:
            continue
        conclusion = str(step.get("conclusion") or "")
        if conclusion in {"success", "failure"}:
            return conclusion
        return None
    return None


def proof_outcomes_from_jobs(jobs: list[dict[str, Any]]) -> dict[str, str]:
    observed: dict[str, str] = {}
    for job in jobs:
        owner = proof_owner_for_job(str(job.get("name") or ""))
        if owner is None:
            continue
        conclusion = semantic_step_conclusion(job, owner)
        if conclusion is None:
            continue
        observed[owner] = conclusion
    return {owner: observed[owner] for owner in PROOF_OWNER_ORDER if owner in observed}


def failed_jobs_from_jobs(jobs: list[dict[str, Any]]) -> list[dict[str, str]]:
    failed: list[dict[str, str]] = []
    for job in jobs:
        owner = proof_owner_for_job(str(job.get("name") or ""))
        if owner is None:
            continue
        if semantic_step_conclusion(job, owner) != "failure":
            continue
        failed.append(
            {
                "owner": owner,
                "name": str(job.get("name") or ""),
                "job_id": str(job.get("id") or ""),
                "step": SEMANTIC_STEP_NAMES[owner],
            }
        )
    return failed


def run_identity(run: dict[str, Any], *, repository: str, workflow_path: str) -> dict[str, str]:
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
    event = observed_text(run.get("event"))
    if event not in ALLOWED_EVENTS:
        raise EvidenceError(f"failed run event {run.get('event')!r} is not a normal-sync event")
    attempt = observed_text(run.get("run_attempt"))
    if attempt is None or not str(attempt).isdigit():
        raise EvidenceError(f"failed run missing exact run_attempt, got {run.get('run_attempt')!r}")
    return {
        "run_id": str(run.get("id") or ""),
        "run_attempt": str(attempt),
        "head_sha": require_full_sha(run.get("head_sha"), field="run.head_sha"),
        "head_branch": observed_text(run.get("head_branch")) or "",
        "event": event,
        "path": path,
        "repository": repository,
    }


def corroborate_identity(evidence: dict[str, Any], identity: dict[str, str]) -> None:
    pairs = (
        ("run_id", "run_id"),
        ("run_attempt", "run_attempt"),
        ("repository_sha", "head_sha"),
        ("head_branch", "head_branch"),
        ("event", "event"),
        ("repository", "repository"),
        ("workflow_path", "path"),
    )
    for evidence_key, identity_key in pairs:
        copy = evidence.get(evidence_key)
        if copy is None:
            continue
        observed = observed_text(copy)
        expected = identity[identity_key]
        if observed is None or observed != expected:
            raise EvidenceError(
                f"evidence {evidence_key}={copy!r} conflicts with run {identity_key}={expected!r}"
            )


def load_control_object(evidence_dir: Path | None, name: str) -> dict[str, Any] | None:
    if evidence_dir is None:
        return None
    path = evidence_dir / "control" / name
    if not path.is_file():
        return None
    return load_json(path)


def canonical_mechanical_facts(
    evidence: dict[str, Any],
    *,
    evidence_dir: Path | None,
) -> dict[str, str]:
    action_inputs = load_control_object(evidence_dir, "action-inputs.json") or {}
    mechanical = load_control_object(evidence_dir, "mechanical-outcome.json") or {}
    facts: dict[str, str] = {}
    for key, sources in (
        ("target_ref", (mechanical.get("target_ref"), action_inputs.get("target_ref"), evidence.get("target_ref"))),
        ("target_kind", (mechanical.get("target_kind"), action_inputs.get("target_kind"), evidence.get("target_kind"))),
        ("target_sha", (mechanical.get("target_sha"), action_inputs.get("target_sha"), evidence.get("target_sha"))),
        ("outcome", (mechanical.get("outcome"), evidence.get("outcome"))),
    ):
        observed = [observed_text(value) for value in sources]
        present = [value for value in observed if value is not None]
        if not present:
            continue
        if any(value != present[0] for value in present):
            raise EvidenceError(f"conflicting {key} copies {present!r}")
        facts[key] = present[0]
    if "target_sha" in facts:
        facts["target_sha"] = require_full_sha(facts["target_sha"], field="target_sha")
    return facts


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
    candidate_present: bool = False,
    worktree_present: bool = False,
    head_branch: str = "",
    event: str = "",
    validation_only: bool | None = None,
    run_attempt: str = "",
    control_files: list[str] | None = None,
    report_files: list[str] | None = None,
    candidate_files: list[str] | None = None,
    mechanical_evidence_present: bool = False,
) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "repository": repository,
        "workflow_path": workflow_path,
        "run_id": str(run_id),
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
        ("run_attempt", run_attempt),
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
    if candidate_files:
        payload["candidate_files"] = list(candidate_files)
    return payload


def pack_candidate_cohort(schema_path: Path, out_dir: Path) -> list[str]:
    copied: list[str] = []
    schema_path = schema_path.resolve()
    if not schema_path.is_dir():
        return copied
    if schema_path.name == "schema":
        cohort = schema_path.parent
        for name in CANDIDATE_DIRS:
            src = cohort / name
            if src.is_dir():
                dest = out_dir / name
                if dest.exists():
                    shutil.rmtree(dest)
                shutil.copytree(src, dest)
                copied.append(name)
        for name in CANDIDATE_FILES:
            if copy_existing_file(cohort / name, out_dir / name):
                copied.append(name)
        return copied
    dest = out_dir / "schema"
    if dest.exists():
        shutil.rmtree(dest)
    shutil.copytree(schema_path, dest)
    copied.append("schema")
    return copied


def reject_non_product_paths(repo_root: Path) -> None:
    status = subprocess.run(
        ["git", "-C", str(repo_root), "status", "--porcelain", "--untracked-files=all"],
        check=True,
        stdout=subprocess.PIPE,
        text=True,
    )
    invalid: list[str] = []
    for line in status.stdout.splitlines():
        path = line[3:].strip() if len(line) > 3 else line.strip()
        if not path:
            continue
        if any(path.startswith(prefix) for prefix in PRODUCT_EXCLUDED_PREFIXES):
            invalid.append(path)
            continue
        if not any(path == prefix.rstrip("/") or path.startswith(prefix) for prefix in PRODUCT_PREFIXES):
            invalid.append(path)
    if invalid:
        rendered = "\n".join(f"- {path}" for path in invalid)
        raise EvidenceError(f"repaired proposal contains paths outside allowed product prefix:\n{rendered}")


def assert_required_logs(log_dir: Path, admission: dict[str, Any]) -> None:
    if admission.get("failure_class") != "protocol-proof":
        return
    missing = [
        owner
        for owner in admission.get("failed_proof_owners") or []
        if not ((log_dir / f"{owner}.log").is_file() and (log_dir / f"{owner}.log").stat().st_size > 0)
    ]
    if missing:
        raise EvidenceError(f"required failed logs missing for {missing}")


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
    candidate: str,
    repo_root: Path,
    head_branch: str = "",
    event: str = "",
    validation_only: bool | None = None,
    run_attempt: str = "",
    module_root: Path | None = None,
    product_only: bool = False,
) -> dict[str, Any]:
    out_dir.mkdir(parents=True, exist_ok=True)
    worktree_present = False
    candidate_present = False
    if product_only:
        reject_non_product_paths(repo_root)
    status = subprocess.run(
        ["git", "-C", str(repo_root), "status", "--porcelain", "--untracked-files=all"],
        check=True,
        stdout=subprocess.PIPE,
        text=True,
    )
    if status.stdout.strip():
        add_args = ["git", "-C", str(repo_root), "add", "-A"]
        if product_only:
            add_args.extend(["--", "codexsdk"])
        subprocess.run(add_args, check=True)
        patch = subprocess.run(
            ["git", "-C", str(repo_root), "diff", "--cached", "--binary"],
            check=True,
            stdout=subprocess.PIPE,
        )
        (out_dir / "protocol-worktree.patch").write_bytes(patch.stdout)
        subprocess.run(["git", "-C", str(repo_root), "reset", "-q"], check=True)
        worktree_present = True
    candidate_files: list[str] = []
    if candidate:
        candidate_files = pack_candidate_cohort(Path(candidate), out_dir)
        candidate_present = bool(candidate_files) or (out_dir / "schema").is_dir()
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
        candidate_present=candidate_present,
        worktree_present=worktree_present,
        head_branch=head_branch,
        event=event,
        validation_only=validation_only,
        run_attempt=run_attempt,
        control_files=control_files,
        report_files=report_files,
        mechanical_evidence_present="escalation.json" in control_files,
        candidate_files=candidate_files,
    )
    write_json(out_dir / EVIDENCE_NAME, evidence)
    return evidence


def validate_artifacts(
    artifacts: list[dict[str, Any]] | None,
    *,
    run_id: str,
    run_attempt: str,
) -> None:
    if artifacts is None:
        return
    seen: set[str] = set()
    for artifact in artifacts:
        name = str(artifact.get("name") or "")
        parsed = parse_artifact_attempt(name)
        if parsed is None:
            raise EvidenceError(
                f"artifact {name!r} is not attempt-addressable for attempt {run_attempt}"
            )
        kind, attempt = parsed
        if attempt != str(run_attempt):
            # GitHub lists every attempt's artifacts on the same run. Other
            # attempts are not evidence for this admission and must not be
            # consumed, but their presence is not a reason to reject.
            continue
        owner = artifact.get("workflow_run")
        owner_id = ""
        if isinstance(owner, dict):
            owner_id = str(owner.get("id") or "")
        if owner_id and owner_id != run_id:
            raise EvidenceError(
                f"artifact {name!r} belongs to run {owner_id}, want {run_id}"
            )
        seen.add(kind)
    required = artifact_name("protocol-sync-evidence", run_attempt)
    if "protocol-sync-evidence" not in seen:
        raise EvidenceError(
            f"missing required artifact {required!r} for attempt {run_attempt}"
        )


def admit(
    *,
    run: dict[str, Any],
    evidence: dict[str, Any],
    repository: str,
    default_branch: str,
    jobs: list[dict[str, Any]] | None = None,
    artifacts: list[dict[str, Any]] | None = None,
    workflow_path: str = SYNC_WORKFLOW_PATH,
    evidence_dir: Path | None = None,
    run_attempt: str = "",
) -> dict[str, Any]:
    identity = run_identity(run, repository=repository, workflow_path=workflow_path)
    selected_attempt = observed_text(run_attempt) or identity["run_attempt"]
    if selected_attempt != identity["run_attempt"]:
        raise EvidenceError(
            f"requested run_attempt {selected_attempt!r} != run.run_attempt {identity['run_attempt']!r}"
        )
    corroborate_identity(evidence, identity)
    validate_artifacts(artifacts, run_id=identity["run_id"], run_attempt=identity["run_attempt"])
    if identity["head_branch"] != observed_text(default_branch):
        raise EvidenceError(
            f"failed run branch {identity['head_branch']!r} is not default branch {default_branch!r}"
        )
    if "validation_only" not in evidence:
        raise EvidenceError("evidence missing observed validation_only")
    if evidence.get("validation_only") is not False:
        raise EvidenceError("validation-only runs are not repairable")
    facts = canonical_mechanical_facts(evidence, evidence_dir=evidence_dir)
    outcome = facts.get("outcome")
    if outcome is None:
        raise EvidenceError("canonical mechanical outcome is unobserved")
    if outcome in NON_REPAIRABLE_OUTCOMES:
        raise EvidenceError(f"outcome {outcome} is not repairable")
    for key in ("target_ref", "target_kind", "target_sha"):
        if key not in facts:
            raise EvidenceError(f"canonical {key} is unobserved")
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
                "applied run with no failed semantic protocol-proof step is not repairable"
            )
        failure_class = "protocol-proof"
    else:
        raise EvidenceError(f"outcome {outcome!r} is not repairable")
    return {
        "failure_class": failure_class,
        "failed_proof_owners": failed_owners,
        "failed_jobs": failed_jobs,
        "proof_outcomes": proof_outcomes,
        "repository": repository,
        "workflow_path": workflow_path,
        "run_id": identity["run_id"],
        "run_attempt": identity["run_attempt"],
        "repository_sha": identity["head_sha"],
        "target_ref": facts["target_ref"],
        "target_kind": facts["target_kind"],
        "target_sha": facts["target_sha"],
        "outcome": outcome,
        "head_branch": identity["head_branch"],
        "event": identity["event"],
        "candidate_present": bool(evidence.get("candidate_present")),
        "worktree_present": bool(evidence.get("worktree_present")),
        "evidence_artifact": artifact_name("protocol-sync-evidence", identity["run_attempt"]),
        "candidate_artifact": artifact_name("protocol-candidate", identity["run_attempt"]),
        "worktree_artifact": artifact_name("protocol-worktree", identity["run_attempt"]),
        "generated_proof_artifact": artifact_name("generated-proof", identity["run_attempt"]),
    }


def write_github_outputs(values: dict[str, str]) -> None:
    output = os.environ.get("GITHUB_OUTPUT")
    if not output:
        return
    with Path(output).open("a", encoding="utf-8") as handle:
        for key, value in values.items():
            handle.write(f"{key}={value}\n")


def parse_optional_bool(value: str | None) -> bool | None:
    if value is None or value == "":
        return None
    if value == "true":
        return True
    if value == "false":
        return False
    raise EvidenceError(f"validation_only {value!r} is not an observed boolean")


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
        candidate=args.candidate,
        repo_root=args.repo_root,
        head_branch=args.head_branch,
        event=args.event,
        validation_only=parse_optional_bool(args.validation_only),
        run_attempt=args.run_attempt,
        module_root=args.module_root,
        product_only=args.product_only,
    )
    write_github_outputs(
        {
            "restore_candidate": "true" if evidence["candidate_present"] else "false",
            "restore_worktree": "true" if evidence["worktree_present"] else "false",
            "run_attempt": str(evidence.get("run_attempt") or ""),
        }
    )
    print(json.dumps(evidence, indent=2, sort_keys=True))
    return 0


def _cmd_require_logs(args: argparse.Namespace) -> int:
    admission = load_json(args.admission)
    assert_required_logs(args.log_dir, admission)
    return 0


def _cmd_admit(args: argparse.Namespace) -> int:
    run = load_json(args.run_json)
    evidence = load_json(args.evidence)
    jobs = load_list(args.jobs_json, "jobs") if args.jobs_json else []
    artifacts = load_list(args.artifacts_json, "artifacts") if args.artifacts_json else None
    evidence_dir = args.evidence.parent
    admission = admit(
        run=run,
        evidence=evidence,
        repository=args.repository,
        default_branch=args.default_branch,
        jobs=jobs,
        artifacts=artifacts,
        workflow_path=args.workflow_path,
        evidence_dir=evidence_dir,
        run_attempt=args.run_attempt,
    )
    write_json(args.out, admission)
    write_github_outputs(
        {
            "repository_sha": str(admission.get("repository_sha") or ""),
            "target_ref": str(admission.get("target_ref") or ""),
            "target_kind": str(admission.get("target_kind") or ""),
            "target_sha": str(admission.get("target_sha") or ""),
            "failure_class": str(admission.get("failure_class") or ""),
            "run_attempt": str(admission.get("run_attempt") or ""),
            "candidate_present": "true" if admission.get("candidate_present") else "false",
            "worktree_present": "true" if admission.get("worktree_present") else "false",
            "evidence_artifact": str(admission.get("evidence_artifact") or ""),
            "candidate_artifact": str(admission.get("candidate_artifact") or ""),
            "worktree_artifact": str(admission.get("worktree_artifact") or ""),
            "generated_proof_artifact": str(admission.get("generated_proof_artifact") or ""),
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
    pack.add_argument("--run-attempt", default="")
    pack.add_argument("--repository-sha", default="")
    pack.add_argument("--target-ref", default="")
    pack.add_argument("--target-kind", default="")
    pack.add_argument("--target-sha", default="")
    pack.add_argument("--outcome", default="")
    pack.add_argument("--head-branch", default="")
    pack.add_argument("--event", default="")
    pack.add_argument("--validation-only", default="")
    pack.add_argument("--candidate", default="")
    pack.add_argument("--product-only", action="store_true")
    pack.set_defaults(func=_cmd_pack)

    logs = sub.add_parser("require-logs", help="fail closed unless required failed-owner logs exist")
    logs.add_argument("--admission", required=True, type=Path)
    logs.add_argument("--log-dir", required=True, type=Path)
    logs.set_defaults(func=_cmd_require_logs)

    admit_cmd = sub.add_parser("admit", help="admit only explicitly repairable failed sync runs")
    admit_cmd.add_argument("--run-json", required=True, type=Path)
    admit_cmd.add_argument("--jobs-json", type=Path)
    admit_cmd.add_argument("--artifacts-json", type=Path)
    admit_cmd.add_argument("--evidence", required=True, type=Path)
    admit_cmd.add_argument("--repository", required=True)
    admit_cmd.add_argument("--default-branch", required=True)
    admit_cmd.add_argument("--run-attempt", default="")
    admit_cmd.add_argument("--workflow-path", default=SYNC_WORKFLOW_PATH)
    admit_cmd.add_argument("--out", required=True, type=Path)
    admit_cmd.set_defaults(func=_cmd_admit)

    args = parser.parse_args()
    return int(args.func(args))


if __name__ == "__main__":
    raise SystemExit(main())

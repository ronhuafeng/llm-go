#!/usr/bin/env python3
"""Run the mechanical-first Codex protocol sync path.

The workflow owns this path. It acquires the upstream target, generates
schemas, applies the mechanical surface, and runs owner-local Go proofs
before any implementation agent is invoked. Unsupported semantic drift
writes explicit escalation evidence instead of asking an agent to
rediscover the repository.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

import codexsdk_sync_changes as sync_changes
import codexsdk_target_policy as target_policy

BASELINE = Path("internal/protocolschema/appserver/v2")
CACHE = Path(".cache/codexsdk-sync")
ACTION_INPUTS = CACHE / "action-inputs.json"
POLICY_OUTPUT = CACHE / "policy.json"
OUTCOME_OUTPUT = CACHE / "mechanical-outcome.json"
ESCALATION_OUTPUT = CACHE / "escalation.json"


class CommandError(RuntimeError):
    def __init__(self, command: list[str], returncode: int, output: str) -> None:
        self.command = command
        self.returncode = returncode
        self.output = output
        super().__init__(f"{' '.join(command)} failed ({returncode})")


def decide_after_policy(policy: dict[str, Any], *, force_compare: bool) -> str:
    decision = policy.get("decision")
    if decision == "block":
        return "blocked"
    if decision == "skip" and not force_compare:
        return "current"
    if decision in {"skip", "allow"}:
        return "generate"
    return "blocked"


def decide_after_drift(*, force_compare: bool, drift_status: str) -> str:
    if force_compare:
        return "comparison" if drift_status == "clean" else "comparison_dirty"
    return "apply"


def decide_after_validate(*, apply_ok: bool, validate_ok: bool, mechanical_only: bool) -> str:
    if apply_ok and validate_ok and mechanical_only:
        return "implemented"
    return "escalate"


def write_json(path: Path, payload: dict[str, Any]) -> dict[str, Any]:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return payload


def write_escalation(
    path: Path,
    *,
    target_ref: str,
    target_kind: str,
    target_sha: str,
    reason: str,
    detail: str,
    artifacts: dict[str, str],
) -> dict[str, Any]:
    return write_json(
        path,
        {
            "target_ref": target_ref,
            "target_kind": target_kind,
            "target_sha": target_sha,
            "reason": reason,
            "detail": detail,
            "artifacts": artifacts,
        },
    )


def write_github_output(values: dict[str, str]) -> None:
    output = os.environ.get("GITHUB_OUTPUT")
    if not output:
        return
    with Path(output).open("a", encoding="utf-8") as handle:
        for key, value in values.items():
            handle.write(f"{key}={value}\n")


def run_command(args: list[str], *, cwd: Path, env: dict[str, str] | None = None) -> str:
    completed = subprocess.run(
        args,
        cwd=cwd,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        check=False,
    )
    if completed.stdout:
        sys.stdout.write(completed.stdout)
        if not completed.stdout.endswith("\n"):
            sys.stdout.write("\n")
    if completed.returncode != 0:
        raise CommandError(args, completed.returncode, completed.stdout)
    return completed.stdout


def load_action_inputs(module_root: Path) -> dict[str, Any]:
    raw = json.loads((module_root / ACTION_INPUTS).read_text(encoding="utf-8"))
    required = ("target_ref", "target_kind", "target_sha", "target_explicit", "allow_downgrade", "force_compare", "upstream_repo")
    missing = [key for key in required if key not in raw]
    if missing:
        raise SystemExit(f"action-inputs.json missing {', '.join(missing)}")
    return raw


def policy_mode() -> str:
    return "scheduled" if os.environ.get("GITHUB_EVENT_NAME") == "schedule" else "manual"


def run_policy(module_root: Path, inputs: dict[str, Any]) -> dict[str, Any]:
    decision = target_policy.evaluate_policy(
        target_policy.load_baseline(module_root / BASELINE / "baseline_metadata.json"),
        target_ref=str(inputs["target_ref"]),
        target_kind=str(inputs["target_kind"]),
        target_sha=str(inputs["target_sha"]),
        target_explicit=bool(inputs["target_explicit"]),
        mode=policy_mode(),
        allow_downgrade=bool(inputs["allow_downgrade"]),
    )
    write_json(module_root / POLICY_OUTPUT, decision)
    return decision


def prepare_upstream_repo(module_root: Path, upstream_repo: str, codex_repo: Path) -> None:
    if codex_repo.exists():
        if not (codex_repo / ".git").exists():
            raise SystemExit(f"cached Codex path is not a Git repository: {codex_repo}")
        origin = run_command(["git", "-C", str(codex_repo), "remote", "get-url", "origin"], cwd=module_root).strip()
        if origin != upstream_repo:
            raise SystemExit(f"cached Codex origin {origin} does not match {upstream_repo}")
        return
    run_command(["git", "init", "--quiet", str(codex_repo)], cwd=module_root)
    run_command(["git", "-C", str(codex_repo), "remote", "add", "origin", upstream_repo], cwd=module_root)


def generate_candidate(module_root: Path, inputs: dict[str, Any]) -> Path:
    target_sha = str(inputs["target_sha"])
    sync_out = module_root / ".cache" / f"codexsdk-upstream-{target_sha[:12]}"
    codex_repo = module_root / ".cache" / "openai-codex"
    rustup_home = module_root / ".cache" / "rustup"
    cargo_home = module_root / ".cache" / "cargo-home"
    cargo_target = module_root / ".cache" / "cargo-target" / "codex"
    for path in (codex_repo.parent, sync_out.parent, rustup_home, cargo_home, cargo_target):
        path.mkdir(parents=True, exist_ok=True)
    prepare_upstream_repo(module_root, str(inputs["upstream_repo"]), codex_repo)
    env = {
        **os.environ,
        "RUSTUP_HOME": str(rustup_home),
        "CARGO_HOME": str(cargo_home),
        "CARGO_TARGET_DIR": str(cargo_target),
    }
    run_command(
        [
            "scripts/codexsdk_track_upstream.sh",
            "--codex-repo",
            str(codex_repo),
            "--commit",
            target_sha,
            "--source-ref",
            str(inputs["target_ref"]),
            "--source-ref-kind",
            str(inputs["target_kind"]),
            "--out",
            str(sync_out),
            "--json",
        ],
        cwd=module_root,
        env=env,
    )
    return sync_out


def apply_candidate(module_root: Path, inputs: dict[str, Any], sync_out: Path) -> None:
    common_sha = (sync_out / "common.rs.source_sha").read_text(encoding="utf-8").strip()
    run_command(
        [
            sys.executable,
            "scripts/codexsdk_apply_sync_candidate.py",
            "--baseline",
            str(BASELINE),
            "--candidate",
            str(sync_out / "schema"),
            "--stable-candidate",
            str(sync_out / "stable-schema"),
            "--codex-repo",
            str(module_root / ".cache" / "openai-codex"),
            "--reports",
            str(sync_out / "reports"),
            "--common-rs",
            str(sync_out / "common.rs"),
            "--common-rs-source-sha",
            common_sha,
            "--target-ref",
            str(inputs["target_ref"]),
            "--target-kind",
            str(inputs["target_kind"]),
            "--target-sha",
            str(inputs["target_sha"]),
            "--json",
        ],
        cwd=module_root,
    )


def capture_mechanical(repo_root: Path, output: Path) -> list[str]:
    payload = sync_changes.capture(repo_root, output, "mechanical")
    paths = payload.get("paths")
    if not isinstance(paths, list) or not all(isinstance(path, str) for path in paths):
        raise ValueError("mechanical change manifest is invalid")
    return paths


def validate_sync(module_root: Path, target_sha: str, candidate: Path) -> None:
    env = {**os.environ, "GOWORK": "off", "PYTHONDONTWRITEBYTECODE": "1"}
    run_command(
        [
            "go",
            "run",
            "./internal/cmd/generatedproof",
            "-expected-upstream-commit",
            target_sha,
        ],
        cwd=module_root,
        env=env,
    )
    run_command(["go", "test", "./..."], cwd=module_root, env=env)
    run_command(
        [sys.executable, "-m", "unittest", "discover", "-s", "scripts", "-p", "*_test.py"],
        cwd=module_root,
        env=env,
    )
    run_command(
        [
            sys.executable,
            "scripts/codexsdk_sync_state.py",
            "--baseline",
            str(BASELINE),
            "--candidate",
            str(candidate),
            "--target-sha",
            target_sha,
        ],
        cwd=module_root,
        env=env,
    )


def emit_outcome(module_root: Path, outcome: str, inputs: dict[str, Any], **extra: str) -> dict[str, Any]:
    payload = {
        "outcome": outcome,
        "target_ref": str(inputs["target_ref"]),
        "target_kind": str(inputs["target_kind"]),
        "target_sha": str(inputs["target_sha"]),
        **extra,
    }
    write_json(module_root / OUTCOME_OUTPUT, payload)
    publish = "true" if outcome == "implemented" else "false"
    escalate = "true" if outcome == "escalate" else "false"
    sync_mode = ""
    if outcome == "implemented":
        sync_mode = "metadata-sync"
    elif outcome == "escalate":
        sync_mode = "repair-sync"
    write_github_output(
        {
            "outcome": outcome,
            "publish": publish,
            "escalate": escalate,
            "sync_mode": sync_mode,
            "target_ref": str(inputs["target_ref"]),
            "target_kind": str(inputs["target_kind"]),
            "target_sha": str(inputs["target_sha"]),
            **extra,
        }
    )
    return payload


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", required=True, type=Path, help="repository root that contains codexsdk/")
    args = parser.parse_args()
    repo_root = args.repo_root.resolve()
    module_root = repo_root / "codexsdk"
    inputs = load_action_inputs(module_root)
    artifacts = {"sync_out": "", "candidate": "", "reports": "", "escalation": str(module_root / ESCALATION_OUTPUT)}

    policy = run_policy(module_root, inputs)
    after_policy = decide_after_policy(policy, force_compare=bool(inputs["force_compare"]))
    if after_policy == "blocked":
        print(policy["reason"], file=sys.stderr)
        emit_outcome(module_root, "blocked", inputs, reason=str(policy["reason"]))
        return 1
    if after_policy == "current":
        emit_outcome(module_root, "current", inputs, reason=str(policy["reason"]))
        return 0

    sync_out = generate_candidate(module_root, inputs)
    artifacts.update(
        {
            "sync_out": str(sync_out),
            "candidate": str(sync_out / "schema"),
            "reports": str(sync_out / "reports"),
        }
    )
    drift = json.loads((sync_out / "reports" / "drift_summary.json").read_text(encoding="utf-8"))
    if drift.get("target", {}).get("source_commit") != inputs["target_sha"]:
        raise SystemExit("candidate source_commit does not match the resolved target")
    after_drift = decide_after_drift(force_compare=bool(inputs["force_compare"]), drift_status=str(drift.get("status") or ""))
    if after_drift in {"comparison", "comparison_dirty"}:
        dirty = sync_changes.changed_paths(repo_root)
        if dirty:
            raise SystemExit("force_compare must leave the protocol worktree unchanged:\n- " + "\n- ".join(dirty))
    if after_drift == "comparison":
        emit_outcome(module_root, "comparison", inputs, reason="read-only comparison found no protocol drift")
        return 0
    if after_drift == "comparison_dirty":
        reason = "read-only comparison found protocol drift; comparison never applies or repairs"
        print(reason, file=sys.stderr)
        emit_outcome(module_root, "comparison_dirty", inputs, reason=reason)
        return 1

    apply_ok = True
    apply_detail = ""
    try:
        apply_candidate(module_root, inputs, sync_out)
    except CommandError as exc:
        apply_ok = False
        apply_detail = exc.output or str(exc)

    mechanical_only = True
    capture_detail = ""
    try:
        if apply_ok:
            capture_mechanical(repo_root, module_root / CACHE / "mechanical-changes.json")
    except ValueError as exc:
        mechanical_only = False
        capture_detail = str(exc)

    validate_ok = True
    validate_detail = ""
    if apply_ok:
        try:
            validate_sync(module_root, str(inputs["target_sha"]), sync_out / "schema")
        except CommandError as exc:
            validate_ok = False
            validate_detail = exc.output or str(exc)

    after_validate = decide_after_validate(
        apply_ok=apply_ok,
        validate_ok=validate_ok,
        mechanical_only=mechanical_only,
    )
    if after_validate == "implemented":
        emit_outcome(module_root, "implemented", inputs, reason="mechanical generation and owner-local Go proofs succeeded")
        return 0

    if not apply_ok:
        reason = "mechanical apply failed with a deterministic incompatibility"
        detail = apply_detail
    elif not mechanical_only:
        reason = "mechanical apply escaped the generated sync surface"
        detail = capture_detail
    else:
        reason = "owner-local validation failed after mechanical apply"
        detail = validate_detail
    write_escalation(
        module_root / ESCALATION_OUTPUT,
        target_ref=str(inputs["target_ref"]),
        target_kind=str(inputs["target_kind"]),
        target_sha=str(inputs["target_sha"]),
        reason=reason,
        detail=detail,
        artifacts=artifacts,
    )
    emit_outcome(module_root, "escalate", inputs, reason=reason)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

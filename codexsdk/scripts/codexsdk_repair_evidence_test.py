#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from io import StringIO
from pathlib import Path
from unittest import mock

sys.path.insert(0, os.path.dirname(__file__))

import codexsdk_repair_evidence as repair


REPO = "ronhuafeng/llm-go"
SHA = "a" * 40
UPSTREAM = "6b9826e3aa83b1a5947db50f4332cb9c65f1b340"
PROOF_JOB_NAMES = {
    "generated": "Protocol proof / Generated reproducibility",
    "owner-local": "Protocol proof / Owner-local tests",
    "schema-state": "Protocol proof / Candidate schema state",
    "script-tests": "Protocol proof / Remaining script tests",
}


def git(cwd: Path, *args: str) -> str:
    return subprocess.run(
        ["git", *args],
        cwd=cwd,
        check=True,
        stdout=subprocess.PIPE,
        text=True,
    ).stdout.strip()


def write_json(path: Path, payload: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def base_run(**overrides: object) -> dict[str, object]:
    payload: dict[str, object] = {
        "id": 123,
        "conclusion": "failure",
        "head_sha": SHA,
        "path": repair.SYNC_WORKFLOW_PATH,
        "head_branch": "main",
        "event": "schedule",
        "head_repository": {"full_name": REPO},
    }
    payload.update(overrides)
    return payload


def base_evidence(**overrides: object) -> dict[str, object]:
    payload = repair.build_evidence(
        repository=REPO,
        workflow_path=repair.SYNC_WORKFLOW_PATH,
        run_id="123",
        repository_sha=SHA,
        target_ref="rust-v0.154.0",
        target_kind="stable_rust_tag",
        target_sha=UPSTREAM,
        outcome="applied",
        applied=True,
        candidate_present=True,
        worktree_present=True,
        head_branch="main",
        event="schedule",
        validation_only=False,
        mechanical_evidence_present=False,
    )
    payload.update(overrides)
    return payload


def proof_jobs(*failed: str, extra: list[dict[str, object]] | None = None) -> list[dict[str, object]]:
    jobs: list[dict[str, object]] = []
    job_id = 10
    for owner in repair.PROOF_OWNER_ORDER:
        jobs.append(
            {
                "id": job_id,
                "name": PROOF_JOB_NAMES[owner],
                "conclusion": "failure" if owner in failed else "success",
            }
        )
        job_id += 1
    if extra:
        jobs.extend(extra)
    return jobs


def run_cli(args: list[str]) -> tuple[int, str]:
    with mock.patch.object(sys, "argv", ["codexsdk_repair_evidence.py", *args]):
        with mock.patch.object(sys, "stdout", StringIO()):
            try:
                return repair.main(), ""
            except SystemExit as exc:
                code = exc.code
                if code in (0, None):
                    return 0, ""
                if isinstance(code, int):
                    return code, str(exc)
                return 1, str(code)


def admit_cli(
    tmp: Path,
    *,
    run: dict[str, object],
    evidence: dict[str, object],
    jobs: list[dict[str, object]] | None = None,
    artifacts: list[dict[str, object]] | None = None,
    default_branch: str = "main",
) -> tuple[int, dict[str, object] | str]:
    write_json(tmp / "run.json", run)
    write_json(tmp / "evidence.json", evidence)
    write_json(tmp / "jobs.json", {"jobs": jobs or []})
    args = [
        "admit",
        "--run-json",
        str(tmp / "run.json"),
        "--jobs-json",
        str(tmp / "jobs.json"),
        "--evidence",
        str(tmp / "evidence.json"),
        "--repository",
        REPO,
        "--default-branch",
        default_branch,
        "--out",
        str(tmp / "admission.json"),
    ]
    if artifacts is not None:
        write_json(tmp / "artifacts.json", {"artifacts": artifacts})
        args.extend(["--artifacts-json", str(tmp / "artifacts.json")])
    code, err = run_cli(args)
    if code == 0:
        return code, json.loads((tmp / "admission.json").read_text(encoding="utf-8"))
    return code, err


class RepairEvidencePackTest(unittest.TestCase):
    def test_pack_writes_only_observed_candidate_and_worktree(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "repo"
            root.mkdir()
            git(root, "init", "-q")
            git(root, "config", "user.email", "ci@example.com")
            git(root, "config", "user.name", "ci")
            (root / "tracked.txt").write_text("base\n", encoding="utf-8")
            git(root, "add", "tracked.txt")
            git(root, "commit", "-q", "-m", "base")
            out = Path(tmp) / "evidence"
            code, _ = run_cli(
                [
                    "pack",
                    "--out",
                    str(out / "clean"),
                    "--repo-root",
                    str(root),
                    "--repository",
                    REPO,
                    "--run-id",
                    "1",
                    "--repository-sha",
                    SHA,
                    "--target-ref",
                    "rust-v0.154.0",
                    "--target-kind",
                    "stable_rust_tag",
                    "--target-sha",
                    UPSTREAM,
                    "--outcome",
                    "current",
                    "--head-branch",
                    "main",
                    "--event",
                    "schedule",
                ]
            )
            self.assertEqual(code, 0)
            clean = json.loads((out / "clean" / "evidence.json").read_text(encoding="utf-8"))
            self.assertFalse(clean["candidate_present"])
            self.assertFalse(clean["worktree_present"])
            self.assertFalse((out / "clean" / "protocol-worktree.patch").exists())
            self.assertFalse((out / "clean" / "schema").exists())
            self.assertNotIn("unknown", json.dumps(clean))

            (root / "tracked.txt").write_text("dirty\n", encoding="utf-8")
            candidate = Path(tmp) / "candidate"
            candidate.mkdir()
            (candidate / "Client.json").write_text("{}\n", encoding="utf-8")
            reports = candidate.parent / "reports"
            reports.mkdir()
            (reports / "drift_summary.json").write_text('{"status":"clean"}\n', encoding="utf-8")
            code, _ = run_cli(
                [
                    "pack",
                    "--out",
                    str(out / "dirty"),
                    "--repo-root",
                    str(root),
                    "--repository",
                    REPO,
                    "--run-id",
                    "2",
                    "--repository-sha",
                    SHA,
                    "--target-ref",
                    "rust-v0.154.0",
                    "--target-kind",
                    "stable_rust_tag",
                    "--target-sha",
                    UPSTREAM,
                    "--outcome",
                    "applied",
                    "--applied",
                    "--candidate",
                    str(candidate),
                    "--head-branch",
                    "main",
                ]
            )
            self.assertEqual(code, 0)
            packed = json.loads((out / "dirty" / "evidence.json").read_text(encoding="utf-8"))
            self.assertTrue(packed["candidate_present"])
            self.assertTrue(packed["worktree_present"])
            self.assertTrue((out / "dirty" / "protocol-worktree.patch").exists())
            self.assertTrue((out / "dirty" / "schema" / "Client.json").exists())
            self.assertEqual(packed["report_files"], ["drift_summary.json"])
            self.assertTrue((out / "dirty" / "reports" / "drift_summary.json").exists())
            self.assertIn("tracked.txt", git(root, "status", "--porcelain"))

    def test_pack_copies_only_existing_mechanical_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "repo"
            module = root / "codexsdk"
            cache = module / ".cache" / "codexsdk-sync"
            cache.mkdir(parents=True)
            git(root, "init", "-q")
            git(root, "config", "user.email", "ci@example.com")
            git(root, "config", "user.name", "ci")
            (root / "tracked.txt").write_text("base\n", encoding="utf-8")
            git(root, "add", "tracked.txt")
            git(root, "commit", "-q", "-m", "base")
            (cache / "action-inputs.json").write_text("{}\n", encoding="utf-8")
            (cache / "escalation.json").write_text('{"reason":"apply failed"}\n', encoding="utf-8")
            out = Path(tmp) / "evidence"
            code, _ = run_cli(
                [
                    "pack",
                    "--out",
                    str(out),
                    "--repo-root",
                    str(root),
                    "--module-root",
                    str(module),
                    "--repository",
                    REPO,
                    "--run-id",
                    "9",
                    "--repository-sha",
                    SHA,
                    "--outcome",
                    "escalate",
                    "--target-ref",
                    "rust-v0.154.0",
                    "--target-kind",
                    "stable_rust_tag",
                    "--target-sha",
                    UPSTREAM,
                ]
            )
            self.assertEqual(code, 0)
            packed = json.loads((out / "evidence.json").read_text(encoding="utf-8"))
            self.assertEqual(packed["control_files"], ["action-inputs.json", "escalation.json"])
            self.assertTrue(packed["mechanical_evidence_present"])
            self.assertTrue((out / "control" / "escalation.json").is_file())
            self.assertFalse((out / "control" / "policy.json").exists())
            self.assertFalse((out / "control" / "mechanical-outcome.json").exists())

    def test_pack_omits_unobserved_and_placeholder_identities(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "repo"
            root.mkdir()
            git(root, "init", "-q")
            git(root, "config", "user.email", "ci@example.com")
            git(root, "config", "user.name", "ci")
            (root / "tracked.txt").write_text("base\n", encoding="utf-8")
            git(root, "add", "tracked.txt")
            git(root, "commit", "-q", "-m", "base")
            out = Path(tmp) / "evidence"
            code, _ = run_cli(
                [
                    "pack",
                    "--out",
                    str(out),
                    "--repo-root",
                    str(root),
                    "--repository",
                    REPO,
                    "--run-id",
                    "3",
                    "--repository-sha",
                    SHA,
                    "--target-ref",
                    "unknown",
                    "--target-kind",
                    "",
                    "--outcome",
                    "unknown",
                ]
            )
            self.assertEqual(code, 0)
            packed = json.loads((out / "evidence.json").read_text(encoding="utf-8"))
            self.assertNotIn("target_ref", packed)
            self.assertNotIn("target_kind", packed)
            self.assertNotIn("target_sha", packed)
            self.assertNotIn("outcome", packed)
            self.assertNotIn("unknown", json.dumps(packed))


class RepairAdmissionTest(unittest.TestCase):
    def test_admit_mechanical_escalate(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, payload = admit_cli(
                Path(tmp),
                run=base_run(),
                evidence=base_evidence(
                    outcome="escalate",
                    applied=False,
                    worktree_present=False,
                    mechanical_evidence_present=True,
                ),
                jobs=[],
                artifacts=[{"name": "protocol-candidate", "workflow_run": {"id": 123}}],
            )
            self.assertEqual(code, 0)
            assert isinstance(payload, dict)
            self.assertEqual(payload["failure_class"], "mechanical")
            self.assertEqual(payload["failed_proof_owners"], [])
            self.assertEqual(payload["proof_outcomes"], {})

    def test_admit_each_failed_proof_owner(self) -> None:
        for owner in repair.PROOF_OWNER_ORDER:
            with self.subTest(owner=owner), tempfile.TemporaryDirectory() as tmp:
                code, payload = admit_cli(
                    Path(tmp),
                    run=base_run(),
                    evidence=base_evidence(),
                    jobs=proof_jobs(owner),
                )
                self.assertEqual(code, 0)
                assert isinstance(payload, dict)
                self.assertEqual(payload["failure_class"], "protocol-proof")
                self.assertEqual(payload["failed_proof_owners"], [owner])
                self.assertEqual(payload["proof_outcomes"][owner], "failure")

    def test_admit_preserves_multiple_failed_proof_owners(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, payload = admit_cli(
                Path(tmp),
                run=base_run(),
                evidence=base_evidence(),
                jobs=proof_jobs("generated", "schema-state"),
            )
            self.assertEqual(code, 0)
            assert isinstance(payload, dict)
            self.assertEqual(payload["failed_proof_owners"], ["generated", "schema-state"])
            self.assertEqual(payload["proof_outcomes"]["owner-local"], "success")
            self.assertEqual(payload["proof_outcomes"]["script-tests"], "success")

    def test_admit_omits_skipped_proof_jobs(self) -> None:
        jobs = proof_jobs("generated")
        jobs[2]["conclusion"] = "skipped"
        with tempfile.TemporaryDirectory() as tmp:
            code, payload = admit_cli(
                Path(tmp),
                run=base_run(),
                evidence=base_evidence(),
                jobs=jobs,
            )
            self.assertEqual(code, 0)
            assert isinstance(payload, dict)
            self.assertNotIn("schema-state", payload["proof_outcomes"])
            self.assertNotIn("skipped", json.dumps(payload["proof_outcomes"]))

    def test_reject_non_repairable_outcomes(self) -> None:
        for outcome in ("blocked", "current", "comparison", "comparison_dirty"):
            with self.subTest(outcome=outcome), tempfile.TemporaryDirectory() as tmp:
                code, err = admit_cli(
                    Path(tmp),
                    run=base_run(),
                    evidence=base_evidence(outcome=outcome, applied=False, worktree_present=False),
                    jobs=[],
                )
                self.assertNotEqual(code, 0)
                self.assertIn(outcome, str(err))

    def test_reject_non_default_branch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, err = admit_cli(
                Path(tmp),
                run=base_run(head_branch="ci/268-native-verification-control-plane"),
                evidence=base_evidence(head_branch="ci/268-native-verification-control-plane"),
                jobs=proof_jobs("generated"),
            )
            self.assertNotEqual(code, 0)
            self.assertIn("default branch", str(err))

    def test_reject_validation_only(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, err = admit_cli(
                Path(tmp),
                run=base_run(),
                evidence=base_evidence(validation_only=True),
                jobs=proof_jobs("generated"),
            )
            self.assertNotEqual(code, 0)
            self.assertIn("validation-only", str(err))

    def test_reject_missing_and_placeholder_target_identity(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            evidence = base_evidence()
            del evidence["target_ref"]
            code, err = admit_cli(Path(tmp), run=base_run(), evidence=evidence, jobs=proof_jobs("generated"))
            self.assertNotEqual(code, 0)
            self.assertIn("target_ref", str(err))
        with tempfile.TemporaryDirectory() as tmp:
            code, err = admit_cli(
                Path(tmp),
                run=base_run(),
                evidence=base_evidence(target_sha="unknown"),
                jobs=proof_jobs("generated"),
            )
            self.assertNotEqual(code, 0)
            self.assertIn("target_sha", str(err))

    def test_reject_missing_candidate(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, err = admit_cli(
                Path(tmp),
                run=base_run(),
                evidence=base_evidence(
                    outcome="escalate",
                    applied=False,
                    candidate_present=False,
                    worktree_present=False,
                    mechanical_evidence_present=True,
                ),
                jobs=[],
            )
            self.assertNotEqual(code, 0)
            self.assertIn("candidate", str(err))

    def test_reject_applied_publish_only_failure(self) -> None:
        jobs = proof_jobs()
        jobs.append({"id": 99, "name": "Publish metadata-sync PR", "conclusion": "failure"})
        with tempfile.TemporaryDirectory() as tmp:
            code, err = admit_cli(
                Path(tmp),
                run=base_run(),
                evidence=base_evidence(),
                jobs=jobs,
            )
            self.assertNotEqual(code, 0)
            self.assertIn("no failed protocol-proof owner", str(err))

    def test_reject_success_and_cancelled(self) -> None:
        for conclusion in ("success", "cancelled"):
            with self.subTest(conclusion=conclusion), tempfile.TemporaryDirectory() as tmp:
                code, err = admit_cli(
                    Path(tmp),
                    run=base_run(conclusion=conclusion),
                    evidence=base_evidence(),
                    jobs=proof_jobs("generated"),
                )
                self.assertNotEqual(code, 0)
                self.assertIn(conclusion, str(err))

    def test_reject_wrong_workflow_repository_sha_and_artifact_owner(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, err = admit_cli(
                Path(tmp),
                run=base_run(path=".github/workflows/other.yml"),
                evidence=base_evidence(),
                jobs=proof_jobs("generated"),
            )
            self.assertNotEqual(code, 0)
            self.assertIn("workflow path", str(err))
        with tempfile.TemporaryDirectory() as tmp:
            code, err = admit_cli(
                Path(tmp),
                run=base_run(head_repository={"full_name": "other/repo"}),
                evidence=base_evidence(),
                jobs=proof_jobs("generated"),
            )
            self.assertNotEqual(code, 0)
            self.assertIn("head repository", str(err))
        with tempfile.TemporaryDirectory() as tmp:
            code, err = admit_cli(
                Path(tmp),
                run=base_run(head_sha="c" * 40),
                evidence=base_evidence(),
                jobs=proof_jobs("generated"),
            )
            self.assertNotEqual(code, 0)
            self.assertIn("head_sha", str(err))
        with tempfile.TemporaryDirectory() as tmp:
            code, err = admit_cli(
                Path(tmp),
                run=base_run(),
                evidence=base_evidence(),
                jobs=proof_jobs("generated"),
                artifacts=[{"name": "protocol-candidate", "workflow_run": {"id": 999}}],
            )
            self.assertNotEqual(code, 0)
            self.assertIn("belongs to run", str(err))


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.dirname(__file__))

import codexsdk_repair_evidence as repair


def git(cwd: Path, *args: str) -> str:
    return subprocess.run(
        ["git", *args],
        cwd=cwd,
        check=True,
        stdout=subprocess.PIPE,
        text=True,
    ).stdout.strip()


class RepairEvidenceTest(unittest.TestCase):
    def test_validate_failed_run_requires_failure_and_matching_identity(self) -> None:
        evidence = repair.build_evidence(
            repository="ronhuafeng/llm-go",
            workflow_path=repair.SYNC_WORKFLOW_PATH,
            run_id="123",
            repository_sha="a" * 40,
            target_ref="rust-v0.154.0",
            target_kind="stable_rust_tag",
            target_sha="b" * 40,
            outcome="applied",
            applied=True,
            candidate_present=True,
            worktree_present=True,
        )
        run = {
            "id": 123,
            "conclusion": "failure",
            "head_sha": "a" * 40,
            "path": repair.SYNC_WORKFLOW_PATH,
            "head_repository": {"full_name": "ronhuafeng/llm-go"},
        }
        loaded = repair.validate_failed_run(
            run=run,
            evidence=evidence,
            repository="ronhuafeng/llm-go",
            artifacts=[{"name": "protocol-candidate", "workflow_run": {"id": 123}}],
        )
        self.assertEqual(loaded["target_ref"], "rust-v0.154.0")

        with self.assertRaises(repair.EvidenceError):
            repair.validate_failed_run(
                run={**run, "conclusion": "success"},
                evidence=evidence,
                repository="ronhuafeng/llm-go",
            )
        with self.assertRaises(repair.EvidenceError):
            repair.validate_failed_run(
                run={**run, "head_sha": "c" * 40},
                evidence=evidence,
                repository="ronhuafeng/llm-go",
            )
        with self.assertRaises(repair.EvidenceError):
            repair.validate_failed_run(
                run={**run, "path": ".github/workflows/other.yml"},
                evidence=evidence,
                repository="ronhuafeng/llm-go",
            )
        with self.assertRaises(repair.EvidenceError):
            repair.validate_failed_run(
                run=run,
                evidence=evidence,
                repository="other/repo",
            )
        with self.assertRaises(repair.EvidenceError):
            repair.validate_failed_run(
                run=run,
                evidence=evidence,
                repository="ronhuafeng/llm-go",
                artifacts=[{"name": "protocol-candidate", "workflow_run": {"id": 999}}],
            )

    def test_validate_does_not_invent_missing_upstream_identity(self) -> None:
        evidence = repair.build_evidence(
            repository="ronhuafeng/llm-go",
            workflow_path=repair.SYNC_WORKFLOW_PATH,
            run_id="123",
            repository_sha="a" * 40,
            target_ref="",
            target_kind="stable_rust_tag",
            target_sha="b" * 40,
            outcome="applied",
            applied=True,
            candidate_present=False,
            worktree_present=False,
        )
        run = {
            "id": 123,
            "conclusion": "failure",
            "head_sha": "a" * 40,
            "path": repair.SYNC_WORKFLOW_PATH,
        }
        with self.assertRaises(repair.EvidenceError):
            repair.validate_failed_run(run=run, evidence=evidence, repository="ronhuafeng/llm-go")

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
            clean = repair.pack_directory(
                out_dir=out / "clean",
                repository="ronhuafeng/llm-go",
                run_id="1",
                repository_sha="a" * 40,
                target_ref="rust-v0.154.0",
                target_kind="stable_rust_tag",
                target_sha="b" * 40,
                outcome="current",
                applied=False,
                candidate="",
                repo_root=root,
            )
            self.assertFalse(clean["candidate_present"])
            self.assertFalse(clean["worktree_present"])
            self.assertFalse((out / "clean" / "protocol-worktree.patch").exists())
            self.assertFalse((out / "clean" / "schema").exists())

            (root / "tracked.txt").write_text("dirty\n", encoding="utf-8")
            candidate = Path(tmp) / "candidate"
            candidate.mkdir()
            (candidate / "Client.json").write_text("{}\n", encoding="utf-8")
            packed = repair.pack_directory(
                out_dir=out / "dirty",
                repository="ronhuafeng/llm-go",
                run_id="2",
                repository_sha="a" * 40,
                target_ref="rust-v0.154.0",
                target_kind="stable_rust_tag",
                target_sha="b" * 40,
                outcome="applied",
                applied=True,
                candidate=str(candidate),
                repo_root=root,
            )
            self.assertTrue(packed["candidate_present"])
            self.assertTrue(packed["worktree_present"])
            self.assertTrue((out / "dirty" / "protocol-worktree.patch").exists())
            self.assertTrue((out / "dirty" / "schema" / "Client.json").exists())
            status = git(root, "status", "--porcelain")
            self.assertIn("tracked.txt", status)


if __name__ == "__main__":
    unittest.main()

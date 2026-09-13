#!/usr/bin/env python3

from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("codexsdk_protocol_summary.py")


def run_summary(tmp: Path, **kwargs: object) -> str:
    jobs = kwargs.pop("jobs")
    generated = kwargs.pop("generated_proof", None)
    jobs_path = tmp / "jobs.json"
    jobs_path.write_text(json.dumps({"jobs": jobs}), encoding="utf-8")
    args = [
        "python3",
        str(SCRIPT),
        "--jobs-json",
        str(jobs_path),
        "--validation-only",
        "true" if kwargs.pop("validation_only", False) else "false",
        "--repository-sha",
        str(kwargs.pop("repository_sha", "aa" * 20)),
        "--target-ref",
        str(kwargs.pop("target_ref", "rust-v0.154.0")),
        "--target-kind",
        str(kwargs.pop("target_kind", "stable_rust_tag")),
        "--target-sha",
        str(kwargs.pop("target_sha", "6b" + "0" * 38)),
        "--outcome",
        str(kwargs.pop("outcome", "current")),
        "--publish-result",
        str(kwargs.pop("publish_result", "skipped")),
    ]
    if generated is not None:
        proof_path = tmp / "generated.json"
        proof_path.write_text(json.dumps(generated), encoding="utf-8")
        args.extend(["--generated-proof", str(proof_path)])
    source_run_id = kwargs.pop("source_run_id", "")
    if source_run_id:
        args.extend(
            [
                "--source-run-id",
                str(source_run_id),
                "--source-run-attempt",
                str(kwargs.pop("source_run_attempt", "2")),
            ]
        )
    if kwargs:
        raise AssertionError(f"unexpected kwargs: {kwargs}")
    out = tmp / "summary.md"
    args.extend(["--out", str(out)])
    completed = subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    if completed.returncode != 0:
        raise AssertionError(completed.stderr)
    return out.read_text(encoding="utf-8")


class ProtocolSummaryTest(unittest.TestCase):
    def test_validation_only_clean_comparison_keeps_identities(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            text = run_summary(
                Path(tmp),
                jobs=[
                    {"name": "Protocol proof / Generated reproducibility", "conclusion": "success"},
                    {"name": "Protocol proof / Owner-local tests", "conclusion": "success"},
                    {"name": "Protocol proof / Candidate schema state", "conclusion": "skipped"},
                    {"name": "Protocol proof / Remaining script tests", "conclusion": "success"},
                ],
                generated_proof={
                    "repository_commit": "aa" * 20,
                    "repository_tree": "bb" * 20,
                    "worktree_overlay": False,
                    "upstream_ref": "rust-v0.154.0",
                    "upstream_ref_kind": "stable_rust_tag",
                    "upstream_commit": "6b" + "0" * 38,
                    "generated_artifacts_reproducible": True,
                    "baseline_path_leak": False,
                },
                validation_only=True,
                publish_result="skipped",
            )
        self.assertIn("repository commit: `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`", text)
        self.assertIn("upstream ref: `rust-v0.154.0`", text)
        self.assertIn("generated_artifacts_reproducible: `true`", text)
        self.assertIn("worktree_overlay: `false`", text)
        self.assertIn("schema_state: `skipped`", text)
        self.assertIn("skipped: validation-only policy", text)
        self.assertIn("does not decide success", text)
        self.assertNotIn("original_ok", text)
        self.assertNotIn("reproof-gate", text)

    def test_generated_mismatch_preserves_unobserved_later_owners(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            text = run_summary(
                Path(tmp),
                jobs=[
                    {"name": "Protocol proof / Generated reproducibility", "conclusion": "failure"},
                ],
                generated_proof={
                    "repository_commit": "aa" * 20,
                    "repository_tree": "bb" * 20,
                    "generated_artifacts_reproducible": False,
                },
                outcome="applied",
                validation_only=False,
                publish_result="skipped",
            )
        self.assertIn("generated: `failure`", text)
        self.assertIn("generated_artifacts_reproducible: `false`", text)
        self.assertIn("owner_local: `unobserved`", text)
        self.assertIn("schema_state: `unobserved`", text)
        self.assertIn("script_tests: `unobserved`", text)
        self.assertIn("skipped: proof did not authorize publication", text)
        self.assertNotIn("owner_local: `failure`", text)

    def test_missing_generated_proof_is_unobserved_not_false(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            text = run_summary(
                Path(tmp),
                jobs=[],
                validation_only=True,
            )
        self.assertIn("generated_artifacts_reproducible: `unobserved`", text)
        self.assertIn("baseline_path_leak: `unobserved`", text)
        self.assertNotIn("generated_artifacts_reproducible: `false`", text)

    def test_repair_summary_names_source_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            text = run_summary(
                Path(tmp),
                jobs=[{"name": "Protocol proof / Generated reproducibility", "conclusion": "success"}],
                source_run_id="12345",
                source_run_attempt="3",
                validation_only=False,
                publish_result="success",
            )
        self.assertIn("source failed run: `12345`", text)
        self.assertIn("source failed run attempt: `3`", text)
        self.assertIn("separate operation from the source failed run", text)

    def test_script_has_no_acceptance_gate(self) -> None:
        source = SCRIPT.read_text(encoding="utf-8")
        for banned in ("original_ok", "reproof_ok", "reproof-gate", "continue-on-error"):
            self.assertNotIn(banned, source)
        self.assertIn("does not authorize publication", source)


if __name__ == "__main__":
    unittest.main()

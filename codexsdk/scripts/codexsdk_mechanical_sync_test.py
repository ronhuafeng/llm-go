#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.dirname(__file__))

import codexsdk_mechanical_sync as mechanical


POLICY_SKIP = {"decision": "skip", "reason": "baseline already points at the selected upstream commit"}
POLICY_ALLOW = {"decision": "allow", "reason": "stable tag moves forward by version"}
POLICY_BLOCK = {"decision": "block", "reason": "stable tag target is older than the current baseline tag"}


class MechanicalSyncDecisionTest(unittest.TestCase):
    def test_skip_without_force_compare_is_current(self) -> None:
        self.assertEqual(mechanical.decide_after_policy(POLICY_SKIP, force_compare=False), "current")

    def test_skip_with_force_compare_still_generates(self) -> None:
        self.assertEqual(mechanical.decide_after_policy(POLICY_SKIP, force_compare=True), "generate")

    def test_allow_generates(self) -> None:
        self.assertEqual(mechanical.decide_after_policy(POLICY_ALLOW, force_compare=False), "generate")

    def test_block_is_blocked(self) -> None:
        self.assertEqual(mechanical.decide_after_policy(POLICY_BLOCK, force_compare=False), "blocked")

    def test_force_compare_clean_is_comparison(self) -> None:
        self.assertEqual(mechanical.decide_after_drift(force_compare=True, drift_status="clean"), "comparison")

    def test_force_compare_dirty_fails_before_escalation(self) -> None:
        self.assertEqual(
            mechanical.decide_after_drift(force_compare=True, drift_status="review-required"),
            "comparison_dirty",
        )

    def test_allowed_drift_applies_mechanically(self) -> None:
        self.assertEqual(mechanical.decide_after_drift(force_compare=False, drift_status="review-required"), "apply")

    def test_clean_mechanical_apply_waits_for_workflow_proofs(self) -> None:
        self.assertEqual(
            mechanical.decide_after_apply(apply_ok=True, mechanical_only=True),
            "applied",
        )

    def test_apply_failure_escalates_before_agent(self) -> None:
        self.assertEqual(
            mechanical.decide_after_apply(apply_ok=False, mechanical_only=True),
            "escalate",
        )

    def test_non_mechanical_changes_escalate(self) -> None:
        self.assertEqual(
            mechanical.decide_after_apply(apply_ok=True, mechanical_only=False),
            "escalate",
        )


class MechanicalSyncEvidenceTest(unittest.TestCase):
    def test_escalation_records_target_and_deterministic_reason(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "escalation.json"
            payload = mechanical.write_escalation(
                path,
                target_ref="rust-v0.154.0",
                target_kind="stable_rust_tag",
                target_sha="a" * 40,
                reason="owner-local validation failed after mechanical apply",
                detail="FAIL: TestGeneratedFacadeZeroValuesFailClosed",
                artifacts={"candidate": "/tmp/schema", "reports": "/tmp/reports"},
            )
            loaded = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(payload, loaded)
            self.assertEqual(loaded["target_ref"], "rust-v0.154.0")
            self.assertEqual(loaded["target_sha"], "a" * 40)
            self.assertIn("validation failed after mechanical apply", loaded["reason"])
            self.assertIn("TestGeneratedFacadeZeroValuesFailClosed", loaded["detail"])
            self.assertEqual(loaded["artifacts"]["reports"], "/tmp/reports")
            self.assertNotIn("rediscover", json.dumps(loaded))

    def test_escalate_github_output_preserves_exact_candidate(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            module = Path(tmp)
            github_output = module / "github-output"
            previous = os.environ.get("GITHUB_OUTPUT")
            os.environ["GITHUB_OUTPUT"] = str(github_output)
            try:
                mechanical.emit_outcome(
                    module,
                    "escalate",
                    {
                        "target_ref": "rust-v0.154.0",
                        "target_kind": "stable_rust_tag",
                        "target_sha": "a" * 40,
                    },
                    reason="mechanical apply failed",
                    **mechanical.candidate_output(Path("/exact/cache/codexsdk-upstream-6b9826e3aa83")),
                )
            finally:
                if previous is None:
                    os.environ.pop("GITHUB_OUTPUT", None)
                else:
                    os.environ["GITHUB_OUTPUT"] = previous
            text = github_output.read_text(encoding="utf-8")
            self.assertIn("candidate=/exact/cache/codexsdk-upstream-6b9826e3aa83/schema", text)
            self.assertIn("escalate=true", text)

    def test_proof_failure_evidence_records_failed_owners_only(self) -> None:
        payload = mechanical.proof_failure_evidence(
            target_ref="rust-v0.154.0",
            target_kind="stable_rust_tag",
            target_sha="a" * 40,
            outcomes={
                "generated-proof": "success",
                "owner-local-tests": "success",
                "schema-state": "failure",
                "script-tests": "success",
            },
            candidate="/exact/schema",
        )
        self.assertEqual(payload["failed_proofs"], ["schema-state"])
        self.assertEqual(
            payload["proof_outcomes"],
            {
                "generated-proof": "success",
                "owner-local-tests": "success",
                "schema-state": "failure",
                "script-tests": "success",
            },
        )
        self.assertEqual(payload["artifacts"]["candidate"], "/exact/schema")

    def test_proof_failure_evidence_omits_unobserved_states(self) -> None:
        payload = mechanical.proof_failure_evidence(
            target_ref="rust-v0.154.0",
            target_kind="stable_rust_tag",
            target_sha="a" * 40,
            outcomes={
                "generated-proof": "failure",
                "owner-local-tests": "skipped",
                "schema-state": "",
                "script-tests": "cancelled",
            },
        )
        self.assertEqual(payload["failed_proofs"], ["generated-proof"])
        self.assertEqual(payload["proof_outcomes"], {"generated-proof": "failure"})
        self.assertNotIn("owner-local-tests", payload["proof_outcomes"])
        self.assertNotIn("schema-state", payload["proof_outcomes"])
        self.assertNotIn("script-tests", payload["proof_outcomes"])
        self.assertNotIn("success", payload["proof_outcomes"].values())

    def test_script_test_failure_is_not_schema_or_generated_success(self) -> None:
        payload = mechanical.proof_failure_evidence(
            target_ref="rust-v0.154.0",
            target_kind="stable_rust_tag",
            target_sha="a" * 40,
            outcomes={
                "generated-proof": "success",
                "owner-local-tests": "success",
                "schema-state": "success",
                "script-tests": "failure",
            },
        )
        self.assertEqual(payload["failed_proofs"], ["script-tests"])
        self.assertEqual(payload["proof_outcomes"]["script-tests"], "failure")


if __name__ == "__main__":
    unittest.main()

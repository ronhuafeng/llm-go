#!/usr/bin/env python3

from __future__ import annotations

import json
import subprocess
import sys
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("codexsdk_sync_workflow_plan.py")


def run_plan(*args: str) -> dict[str, object]:
    completed = subprocess.run(
        [sys.executable, str(SCRIPT), *args, "--json"],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    return json.loads(completed.stdout)


class SyncWorkflowPlanTest(unittest.TestCase):
    def test_pre_drift_plan_preserves_skip_block_and_allow(self) -> None:
        skip = run_plan("--policy-decision", "skip", "--force-compare", "false")
        block = run_plan("--policy-decision", "block", "--force-compare", "false")
        allow = run_plan("--policy-decision", "allow", "--force-compare", "false")

        self.assertEqual(skip["mode"], "noop")
        self.assertFalse(skip["should_detect"])
        self.assertEqual(block["mode"], "blocked")
        self.assertFalse(block["should_detect"])
        self.assertEqual(allow["mode"], "detect")
        self.assertTrue(allow["should_detect"])

    def test_forced_comparison_fails_when_target_policy_blocks(self) -> None:
        plan = run_plan(
            "--policy-decision",
            "block",
            "--force-compare",
            "true",
        )

        self.assertEqual(plan["mode"], "verification-fail")
        self.assertFalse(plan["should_detect"])
        self.assertTrue(plan["verification_failed"])

    def test_forced_comparison_detects_when_baseline_is_current(self) -> None:
        plan = run_plan(
            "--policy-decision",
            "skip",
            "--force-compare",
            "true",
        )

        self.assertEqual(plan["mode"], "verify")
        self.assertTrue(plan["should_detect"])
        self.assertFalse(plan["verification_failed"])

    def test_clean_forward_target_still_requires_agent_pass(self) -> None:
        plan = run_plan(
            "--policy-decision",
            "allow",
            "--force-compare",
            "false",
            "--drift-status",
            "clean",
        )

        self.assertEqual(plan["mode"], "metadata-sync")
        self.assertTrue(plan["needs_apply"])
        self.assertTrue(plan["needs_agent"])
        self.assertTrue(plan["needs_publish"])

    def test_drifted_forward_target_plans_repair_sync(self) -> None:
        plan = run_plan(
            "--policy-decision",
            "allow",
            "--force-compare",
            "false",
            "--drift-status",
            "review-required",
        )

        self.assertEqual(plan["mode"], "repair-sync")
        self.assertTrue(plan["needs_apply"])
        self.assertTrue(plan["needs_agent"])
        self.assertTrue(plan["needs_publish"])

    def test_forced_comparison_fails_when_drift_remains(self) -> None:
        plan = run_plan(
            "--policy-decision",
            "allow",
            "--force-compare",
            "true",
            "--drift-status",
            "review-required",
        )

        self.assertEqual(plan["mode"], "verification-fail")
        self.assertTrue(plan["verification_failed"])


if __name__ == "__main__":
    unittest.main()

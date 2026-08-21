#!/usr/bin/env python3

from __future__ import annotations

import unittest

from codexsdk_finalize_sweep import resolve_metadata, select_candidate


SYNC_BODY = """<!-- codexsdk-upstream-sync
phase: fix
upstream_ref: rust-v0.141.0
upstream_ref_kind: stable_rust_tag
upstream_commit: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
drift_sha256: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
sync_commit: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
base_branch: main
-->

Automated upstream protocol sync.
"""


class FinalizeSweepTest(unittest.TestCase):
    def test_selects_historical_default_branch_sync_pr(self) -> None:
        result, skipped = select_candidate(
            active_runs=[],
            finalized_tags={},
            prs=[
                {
                    "number": 31,
                    "body": SYNC_BODY,
                    "mergeCommit": {"oid": "c" * 40},
                    "url": "https://github.example/repo/pull/31",
                }
            ],
        )

        self.assertEqual(result, {"dispatch": "true", "pr_number": "31"})
        self.assertEqual(skipped, [])

    def test_skips_sync_pr_when_upstream_tag_already_points_at_merge_commit(self) -> None:
        result, skipped = select_candidate(
            active_runs=[],
            finalized_tags={"upstream-codex-rust-v0.141.0": "c" * 40},
            prs=[
                {
                    "number": 31,
                    "body": SYNC_BODY,
                    "mergeCommit": {"oid": "c" * 40},
                    "url": "https://github.example/repo/pull/31",
                }
            ],
        )

        self.assertEqual(result, {"dispatch": "false"})
        self.assertEqual(
            skipped,
            ["PR #31: upstream-codex-rust-v0.141.0 already points at the merge commit"],
        )

    def test_does_not_treat_nonstable_ref_as_a_completed_sync_tag(self) -> None:
        body = SYNC_BODY.replace("rust-v0.141.0", "main").replace(
            "stable_rust_tag", "git_ref"
        )
        result, skipped = select_candidate(
            active_runs=[],
            finalized_tags={"upstream-codex-main": "c" * 40},
            prs=[
                {
                    "number": 31,
                    "body": body,
                    "mergeCommit": {"oid": "c" * 40},
                    "url": "https://github.example/repo/pull/31",
                }
            ],
        )

        self.assertEqual(result, {"dispatch": "true", "pr_number": "31"})
        self.assertEqual(skipped, [])

    def test_resolves_metadata_from_pr_body(self) -> None:
        result = resolve_metadata(
            pr={
                "number": 31,
                "state": "MERGED",
                "body": SYNC_BODY,
                "baseRefName": "",
                "mergeCommit": {"oid": "c" * 40},
                "url": "https://github.example/repo/pull/31",
            },
            inputs={},
            default_branch="main",
        )

        self.assertEqual(result["drift_sha"], "d" * 64)
        self.assertEqual(result["landed_commit"], "c" * 40)
        self.assertEqual(result["landed_ref"], "main")
        self.assertEqual(result["pr_number"], "31")
        self.assertEqual(result["upstream_ref"], "rust-v0.141.0")
        self.assertEqual(result["upstream_ref_kind"], "stable_rust_tag")
        self.assertEqual(result["upstream_sha"], "a" * 40)


if __name__ == "__main__":
    unittest.main()

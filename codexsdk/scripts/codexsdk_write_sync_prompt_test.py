#!/usr/bin/env python3

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))

import codexsdk_write_sync_prompt as sync_prompt


class SyncPromptTest(unittest.TestCase):
    def test_default_template_remains_module_owned_with_root_relative_agent_paths(self) -> None:
        prompt = sync_prompt.build_prompt(
            auto_sync_dir="codexsdk/.cache/codexsdk-auto-sync",
            candidate_dir="/tmp/codexsdk-upstream",
            land_ref="main",
            upstream_ref="rust-v1.2.3",
            upstream_ref_kind="stable_rust_tag",
            upstream_sha="1" * 40,
        )

        self.assertTrue(sync_prompt.DEFAULT_TEMPLATE.is_file())
        self.assertIn(
            "codexsdk/.agents/skills/codexsdk-sync-upstream/SKILL.md",
            prompt,
        )
        self.assertIn(
            "codexsdk/.cache/codexsdk-auto-sync/apply-result.json",
            prompt,
        )


if __name__ == "__main__":
    unittest.main()

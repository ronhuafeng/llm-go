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

import codexsdk_apply_sync_candidate as apply_sync


def write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


class ApplySyncCandidateTest(unittest.TestCase):
    def test_common_rs_source_sha_must_match_target_sha(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            common_rs = Path(tmp) / "common.rs"
            common_rs.write_text("client_request_definitions! {}\n", encoding="utf-8")

            with self.assertRaisesRegex(ValueError, "does not match target"):
                apply_sync.verify_common_rs_provenance(common_rs, "1" * 40, "2" * 40)

    def test_common_rs_content_must_match_target_commit_when_repo_is_available(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            codex_repo = root / "codex"
            common_path = codex_repo / apply_sync.COMMON_RS_REF
            common_path.parent.mkdir(parents=True)
            common_path.write_text("client_request_definitions! {}\n", encoding="utf-8")
            subprocess.run(["git", "init", "-q", str(codex_repo)], check=True)
            subprocess.run(["git", "-C", str(codex_repo), "config", "user.email", "codex@example.com"], check=True)
            subprocess.run(["git", "-C", str(codex_repo), "config", "user.name", "Codex"], check=True)
            subprocess.run(["git", "-C", str(codex_repo), "add", apply_sync.COMMON_RS_REF], check=True)
            subprocess.run(["git", "-C", str(codex_repo), "commit", "-q", "-m", "common"], check=True)
            target_sha = subprocess.check_output(
                ["git", "-C", str(codex_repo), "rev-parse", "HEAD"],
                text=True,
            ).strip()

            candidate_common = root / "common.rs"
            candidate_common.write_text("client_request_definitions! {}\n", encoding="utf-8")
            apply_sync.verify_common_rs_provenance(candidate_common, target_sha, target_sha, codex_repo)

            candidate_common.write_text("server_request_definitions! {}\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "content does not match"):
                apply_sync.verify_common_rs_provenance(candidate_common, target_sha, target_sha, codex_repo)

    def test_build_coverage_seeds_missing_fields_for_changed_object_schemas(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_json(
                root / "ChangedResponse.json",
                {
                    "title": "ChangedResponse",
                    "type": "object",
                    "required": ["value"],
                    "properties": {
                        "optional": {"type": "boolean"},
                        "value": {"type": "string"},
                    },
                },
            )
            old_coverage = {
                "fields": [],
                "methods": [],
                "schema_version": 1,
                "types": [{"schema": "ChangedResponse.json", "stability": "stable"}],
                "valid_statuses": ["supported", "supported-generated", "deferred", "intentionally-unsupported"],
            }
            coverage = apply_sync.build_coverage(
                root,
                old_coverage,
                {"entries": []},
                {"ChangedResponse.json"},
            )

            fields = {item["path"]: item for item in coverage["fields"]}
            self.assertEqual(
                fields["ChangedResponse.json#/properties/value"]["type"],
                "ChangedResponse",
            )
            self.assertTrue(fields["ChangedResponse.json#/properties/value"]["required"])
            self.assertFalse(fields["ChangedResponse.json#/properties/optional"]["required"])

    def test_build_coverage_preserves_existing_reviewed_field_for_changed_schema(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_json(
                root / "ChangedResponse.json",
                {
                    "title": "ChangedResponse",
                    "type": "object",
                    "required": ["value"],
                    "properties": {"value": {"type": "string"}},
                },
            )
            old_field = {
                "field": "value",
                "owner": "codex-go-sdk",
                "path": "ChangedResponse.json#/properties/value",
                "reason": "Reviewed custom field coverage.",
                "required": True,
                "schema": "ChangedResponse.json",
                "stability": "stable",
                "status": "supported",
                "type": "ChangedResponse",
            }
            coverage = apply_sync.build_coverage(
                root,
                {
                    "fields": [old_field],
                    "methods": [],
                    "schema_version": 1,
                    "types": [{"schema": "ChangedResponse.json", "stability": "stable"}],
                    "valid_statuses": ["supported", "supported-generated", "deferred", "intentionally-unsupported"],
                },
                {"entries": []},
                {"ChangedResponse.json"},
            )

            self.assertEqual(coverage["fields"], [old_field])


if __name__ == "__main__":
    unittest.main()

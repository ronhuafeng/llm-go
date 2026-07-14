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

import codexsdk_schema_utils as schema_utils
import codexsdk_sync_state as sync_state


SOURCE_SHA = "1" * 40


def write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def aggregate(method: str = "thread/start", params: str = "ThreadStartParams") -> dict[str, object]:
    return {
        "oneOf": [
            {
                "properties": {
                    "method": {"enum": [method]},
                    "params": {"$ref": f"#/definitions/{params}"},
                }
            }
        ]
    }


def write_valid_baseline(root: Path) -> None:
    write_json(root / "ClientRequest.json", aggregate())
    write_json(root / "ServerRequest.json", {"oneOf": []})
    write_json(root / "ServerNotification.json", {"oneOf": []})
    write_json(root / "ClientNotification.json", {"oneOf": []})
    write_json(root / "ThreadStartResponse.json", {"type": "object", "properties": {"value": {"type": "string"}}})

    checksum = schema_utils.schema_bundle_sha256(root)
    metadata = {
        "schema_bundle_sha256": checksum,
        "schema_file_count": len(schema_utils.schema_files(root)),
        "source_commit": SOURCE_SHA,
        "source_ref_name": "rust-v0.141.0",
        "source_ref_kind": "stable_rust_tag",
    }
    write_json(root / "baseline_metadata.json", metadata)
    write_json(
        root / "drift_report.json",
        {
            "status": "clean",
            "comparison_mode": "canonical-json",
            "target": {
                "source_commit": SOURCE_SHA,
                "source_ref_name": "rust-v0.141.0",
                "source_ref_kind": "stable_rust_tag",
                "schema_bundle_sha256": checksum,
            },
            "file_diff": {"added": [], "changed": [], "removed": []},
            "method_diff": {
                "ClientRequest.json": {"added": [], "removed": []},
                "ServerRequest.json": {"added": [], "removed": []},
                "ServerNotification.json": {"added": [], "removed": []},
                "ClientNotification.json": {"added": [], "removed": []},
            },
            "matrix_update_skeleton": "matrix_update_skeleton.json",
        },
    )
    write_json(
        root / "matrix_update_skeleton.json",
        {
            "status": "empty",
            "source": "drift_summary.json",
            "method_updates": [],
            "type_updates": [],
            "field_updates": [],
            "valid_statuses": ["supported", "supported-generated", "deferred", "intentionally-unsupported"],
        },
    )
    write_json(
        root / "manifest.json",
        {
            "status": "classified-manifest",
            "aggregate_schemas": [
                "ClientRequest.json",
                "ServerRequest.json",
                "ServerNotification.json",
                "ClientNotification.json",
            ],
            "entries": [
                {
                    "method": "thread/start",
                    "source_schema": "ClientRequest.json",
                    "params_or_payload_schema": "ThreadStartParams",
                    "response_schema": "ThreadStartResponse.json",
                }
            ],
        },
    )
    write_json(
        root / "coverage_matrix.json",
        {
            "status": "classified-manifest",
            "valid_statuses": ["supported", "supported-generated", "deferred", "intentionally-unsupported"],
            "methods": [{"method": "thread/start", "source_schema": "ClientRequest.json"}],
            "types": [{"schema": "ThreadStartResponse.json"}],
            "fields": [
                {
                    "schema": "ThreadStartResponse.json",
                    "path": "ThreadStartResponse.json#/properties/value",
                    "field": "value",
                }
            ],
        },
    )


def codes(findings: list[sync_state.Finding]) -> set[str]:
    return {item.code for item in findings}


class SyncStateTest(unittest.TestCase):
    def test_current_checked_in_baseline_passes(self) -> None:
        repo = Path(__file__).resolve().parents[1]
        baseline = repo / "codexsdk/internal/protocolschema/appserver/v2"
        findings = sync_state.validate_baseline(baseline)
        self.assertEqual(findings, [])

    def test_valid_fixture_passes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)
            self.assertEqual(sync_state.validate_baseline(root), [])

    def test_generated_facade_target_requires_explicit_status(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)
            manifest_path = root / "manifest.json"
            manifest = schema_utils.load_json(manifest_path)
            manifest["entries"][0].update(
                {
                    "direction": "client_to_server",
                    "kind": "request",
                    "facade_target": "Threads().Start",
                }
            )
            write_json(manifest_path, manifest)

            self.assertIn("manifest_invalid_facade_status", codes(sync_state.validate_baseline(root)))

    def test_client_request_rejects_facade_target_typo(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)
            manifest_path = root / "manifest.json"
            manifest = schema_utils.load_json(manifest_path)
            manifest["entries"][0].update(
                {
                    "direction": "client_to_server",
                    "kind": "request",
                    "facade_target": "Threads.Start",
                    "facade_status": "generated",
                }
            )
            write_json(manifest_path, manifest)

            self.assertIn("manifest_invalid_facade_target", codes(sync_state.validate_baseline(root)))

    def test_bad_metadata_count_and_checksum_fail(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)
            metadata_path = root / "baseline_metadata.json"
            metadata = schema_utils.load_json(metadata_path)
            metadata["schema_file_count"] = 999
            metadata["schema_bundle_sha256"] = "bad"
            write_json(metadata_path, metadata)

            self.assertTrue({"metadata_schema_count", "metadata_schema_bundle_sha256"} <= codes(sync_state.validate_baseline(root)))

    def test_dirty_drift_report_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)
            drift_path = root / "drift_report.json"
            drift = schema_utils.load_json(drift_path)
            drift["status"] = "review-required"
            drift["file_diff"]["changed"] = ["ThreadStartResponse.json"]
            write_json(drift_path, drift)

            self.assertTrue({"drift_report_dirty", "drift_report_file_diff"} <= codes(sync_state.validate_baseline(root)))

    def test_local_paths_in_metadata_or_reports_fail(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)
            metadata_path = root / "baseline_metadata.json"
            metadata = schema_utils.load_json(metadata_path)
            metadata["source_repo"] = "/Users/example/openai-codex"
            write_json(metadata_path, metadata)

            self.assertIn("local_path", codes(sync_state.validate_baseline(root)))

    def test_stale_manifest_method_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)
            manifest_path = root / "manifest.json"
            manifest = schema_utils.load_json(manifest_path)
            manifest["entries"].append(
                {
                    "method": "thread/deleted",
                    "source_schema": "ClientRequest.json",
                    "params_or_payload_schema": "ThreadDeletedParams",
                    "response_schema": "",
                }
            )
            write_json(manifest_path, manifest)

            self.assertIn("manifest_stale_method", codes(sync_state.validate_baseline(root)))

    def test_schema_v2_manifest_requires_classified_unique_surface(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)
            manifest_path = root / "manifest.json"
            manifest = schema_utils.load_json(manifest_path)
            manifest["schema_version"] = 2
            manifest["surface"] = [
                {"kind": "field", "name": "Event.ID", "signature": "string", "stability": ""},
                {"kind": "field", "name": "Event.ID", "signature": "string", "stability": "mixed"},
                {"kind": "field", "name": "Event.ID", "signature": "string", "stability": "stable"},
            ]
            write_json(manifest_path, manifest)

            self.assertTrue(
                {"manifest_unclassified_surface", "manifest_invalid_surface", "manifest_duplicate_surface"}
                <= codes(sync_state.validate_baseline(root))
            )

    def test_stale_coverage_method_type_and_field_fail(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)
            coverage_path = root / "coverage_matrix.json"
            coverage = schema_utils.load_json(coverage_path)
            coverage["methods"].append({"method": "thread/deleted"})
            coverage["types"].append({"schema": "Missing.json"})
            coverage["fields"].append({"schema": "ThreadStartResponse.json", "path": "ThreadStartResponse.json#/properties/missing"})
            write_json(coverage_path, coverage)

            self.assertTrue(
                {"coverage_stale_method", "coverage_stale_type_schema", "coverage_stale_field_pointer"}
                <= codes(sync_state.validate_baseline(root))
            )

    def test_candidate_mismatch_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            baseline = root / "baseline"
            candidate = root / "candidate"
            write_valid_baseline(baseline)
            write_valid_baseline(candidate)
            write_json(candidate / "ThreadStartResponse.json", {"type": "object", "properties": {"other": {"type": "string"}}})

            self.assertIn("candidate_schema_mismatch", codes(sync_state.validate_baseline(baseline, candidate=candidate)))

    def test_cli_success_is_silent_by_default(self) -> None:
        repo = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)

            completed = subprocess.run(
                [
                    sys.executable,
                    str(repo / "scripts/codexsdk_sync_state.py"),
                    "--baseline",
                    str(root),
                ],
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            self.assertEqual(completed.stdout, "")
            self.assertEqual(completed.stderr, "")

    def test_cli_failure_writes_findings_to_stderr(self) -> None:
        repo = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)
            metadata_path = root / "baseline_metadata.json"
            metadata = schema_utils.load_json(metadata_path)
            metadata["schema_file_count"] = 999
            write_json(metadata_path, metadata)

            completed = subprocess.run(
                [
                    sys.executable,
                    str(repo / "scripts/codexsdk_sync_state.py"),
                    "--baseline",
                    str(root),
                ],
                check=False,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            self.assertEqual(completed.returncode, 1)
            self.assertEqual(completed.stdout, "")
            self.assertIn("metadata_schema_count", completed.stderr)

    def test_cli_json_writes_structured_findings_to_stdout(self) -> None:
        repo = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_valid_baseline(root)

            completed = subprocess.run(
                [
                    sys.executable,
                    str(repo / "scripts/codexsdk_sync_state.py"),
                    "--baseline",
                    str(root),
                    "--json",
                ],
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            payload = json.loads(completed.stdout)
            self.assertEqual(payload["status"], "ok")
            self.assertEqual(payload["finding_count"], 0)
            self.assertEqual(completed.stderr, "")


if __name__ == "__main__":
    unittest.main()

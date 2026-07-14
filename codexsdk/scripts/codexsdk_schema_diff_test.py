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

import codexsdk_schema_diff as schema_diff


def write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def aggregate(methods: list[str]) -> dict[str, object]:
    return {
        "oneOf": [
            {
                "properties": {
                    "method": {"enum": [method]},
                    "params": {"$ref": f"#/definitions/{method.title().replace('/', '')}Params"},
                }
            }
            for method in methods
        ]
    }


def write_schema_set(root: Path, *, client_methods: list[str], extra: dict[str, object] | None = None) -> None:
    write_json(root / "ClientRequest.json", aggregate(client_methods))
    write_json(root / "ServerRequest.json", aggregate([]))
    write_json(root / "ServerNotification.json", aggregate([]))
    write_json(root / "ClientNotification.json", aggregate([]))
    write_json(root / "Shared.json", {"type": "object", "properties": {"value": {"type": "string"}}})
    if extra:
        for rel, value in extra.items():
            write_json(root / rel, value)
    write_json(root / "baseline_metadata.json", {"ignored": root.name})


class SchemaDiffTest(unittest.TestCase):
    def test_clean_schemas_produce_clean_report_and_ignore_metadata_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            baseline = root / "baseline"
            candidate = root / "candidate"
            write_schema_set(baseline, client_methods=["thread/start"])
            write_schema_set(candidate, client_methods=["thread/start"])
            write_json(candidate / "baseline_metadata.json", {"ignored": "different"})

            drift, matrix, _ = schema_diff.build_reports(
                baseline=baseline,
                candidate=candidate,
                reports=root / "reports",
                source_commit="1" * 40,
                source_ref="rust-v0.141.0",
                source_ref_kind="stable_rust_tag",
                codex_version="codex-cli 0.141.0",
                generator="cargo",
                generator_detail="cargo run",
            )

            self.assertEqual(drift["status"], "clean")
            self.assertEqual(drift["file_diff"], {"added": [], "changed": [], "removed": []})
            self.assertEqual(matrix["status"], "empty")

    def test_schema_file_diff_reports_added_changed_and_removed_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            baseline = root / "baseline"
            candidate = root / "candidate"
            write_schema_set(baseline, client_methods=[], extra={"Removed.json": {"type": "string"}})
            write_schema_set(candidate, client_methods=[], extra={"Added.json": {"type": "string"}})
            write_json(candidate / "Shared.json", {"type": "object", "properties": {"value": {"type": "number"}}})

            drift, matrix, _ = schema_diff.build_reports(
                baseline=baseline,
                candidate=candidate,
                reports=root / "reports",
                source_commit="1" * 40,
                source_ref="manual",
                source_ref_kind="manual_ref",
                codex_version="codex-cli test",
                generator="compare-only",
                generator_detail="candidate schema",
            )

            self.assertEqual(drift["status"], "review-required")
            self.assertEqual(drift["file_diff"]["added"], ["Added.json"])
            self.assertEqual(drift["file_diff"]["changed"], ["Shared.json"])
            self.assertEqual(drift["file_diff"]["removed"], ["Removed.json"])
            self.assertEqual(matrix["status"], "review-required")

    def test_aggregate_method_diff_reports_added_and_removed_methods(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            baseline = root / "baseline"
            candidate = root / "candidate"
            write_schema_set(baseline, client_methods=["thread/start"])
            write_schema_set(candidate, client_methods=["thread/resume"])

            drift, matrix, _ = schema_diff.build_reports(
                baseline=baseline,
                candidate=candidate,
                reports=root / "reports",
                source_commit="1" * 40,
                source_ref="manual",
                source_ref_kind="manual_ref",
                codex_version="codex-cli test",
                generator="compare-only",
                generator_detail="candidate schema",
            )

            diff = drift["method_diff"]["ClientRequest.json"]
            self.assertEqual(diff["added"], ["thread/resume"])
            self.assertEqual(diff["removed"], ["thread/start"])
            self.assertEqual(matrix["method_updates"][0]["status"], "review-required")

    def test_reports_mode_is_stdout_silent_by_default(self) -> None:
        repo = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            baseline = root / "baseline"
            candidate = root / "candidate"
            reports = root / "reports"
            write_schema_set(baseline, client_methods=["thread/start"])
            write_schema_set(candidate, client_methods=["thread/start"])

            completed = subprocess.run(
                [
                    sys.executable,
                    str(repo / "scripts/codexsdk_schema_diff.py"),
                    "--baseline",
                    str(baseline),
                    "--candidate",
                    str(candidate),
                    "--reports",
                    str(reports),
                    "--source-commit",
                    "1" * 40,
                    "--source-ref",
                    "rust-v0.141.0",
                    "--source-ref-kind",
                    "stable_rust_tag",
                    "--codex-version",
                    "codex-cli test",
                    "--generator",
                    "compare-only",
                    "--generator-detail",
                    "candidate schema",
                ],
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            self.assertEqual(completed.stdout, "")
            self.assertEqual(completed.stderr, "")
            self.assertTrue((reports / "drift_summary.json").exists())

    def test_json_mode_prints_drift_json_without_writing_reports(self) -> None:
        repo = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            baseline = root / "baseline"
            candidate = root / "candidate"
            write_schema_set(baseline, client_methods=["thread/start"])
            write_schema_set(candidate, client_methods=["thread/start"])

            completed = subprocess.run(
                [
                    sys.executable,
                    str(repo / "scripts/codexsdk_schema_diff.py"),
                    "--baseline",
                    str(baseline),
                    "--candidate",
                    str(candidate),
                    "--source-commit",
                    "1" * 40,
                    "--source-ref",
                    "rust-v0.141.0",
                    "--source-ref-kind",
                    "stable_rust_tag",
                    "--codex-version",
                    "codex-cli test",
                    "--generator",
                    "compare-only",
                    "--generator-detail",
                    "candidate schema",
                    "--json",
                ],
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            payload = json.loads(completed.stdout)
            self.assertEqual(payload["status"], "clean")
            self.assertEqual(completed.stderr, "")
            self.assertFalse((root / "reports").exists())

    def test_verbose_mode_writes_human_output_to_stderr(self) -> None:
        repo = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            baseline = root / "baseline"
            candidate = root / "candidate"
            reports = root / "reports"
            write_schema_set(baseline, client_methods=["thread/start"])
            write_schema_set(candidate, client_methods=["thread/start"])

            completed = subprocess.run(
                [
                    sys.executable,
                    str(repo / "scripts/codexsdk_schema_diff.py"),
                    "--baseline",
                    str(baseline),
                    "--candidate",
                    str(candidate),
                    "--reports",
                    str(reports),
                    "--source-commit",
                    "1" * 40,
                    "--source-ref",
                    "rust-v0.141.0",
                    "--source-ref-kind",
                    "stable_rust_tag",
                    "--codex-version",
                    "codex-cli test",
                    "--generator",
                    "compare-only",
                    "--generator-detail",
                    "candidate schema",
                    "--verbose",
                ],
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            self.assertEqual(completed.stdout, "")
            self.assertIn("status=clean", completed.stderr)
            self.assertIn("drift_summary.json", completed.stderr)


if __name__ == "__main__":
    unittest.main()

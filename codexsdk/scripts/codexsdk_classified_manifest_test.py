#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.dirname(__file__))

import codexsdk_classified_manifest as classified_manifest


def write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


class ClassifiedManifestTest(unittest.TestCase):
    def test_prepare_generation_root_filters_by_schema_and_pointer(self) -> None:
        with tempfile.TemporaryDirectory() as raw_tmp:
            root = Path(raw_tmp)
            complete = root / "complete"
            stable = root / "stable"
            output = root / "output"
            write_json(stable / "Stable.json", {"type": "object", "properties": {"kept": {"type": "string"}}})
            write_json(
                complete / "coverage_matrix.json",
                {
                    "methods": [
                        {"method": "stable/read", "stability": "experimental"},
                        {"method": "preview/read", "stability": "stable"},
                    ],
                    "types": [{"schema": "Stable.json"}, {"schema": "Preview.json"}],
                    "fields": [
                        {"schema": "Stable.json", "path": "Stable.json#/properties/kept"},
                        {"schema": "Stable.json", "path": "Stable.json#/properties/removed"},
                    ],
                },
            )
            write_json(
                complete / "manifest.json",
                {
                    "entries": [
                        {"method": "stable/read", "stability": "stable"},
                        {"method": "preview/read", "stability": "experimental"},
                    ],
                    "classification_sources": {},
                },
            )

            classified_manifest.prepare_generation_root(stable, complete, output)

            coverage = classified_manifest.load_json(output / "coverage_matrix.json")
            manifest = classified_manifest.load_json(output / "manifest.json")
            self.assertEqual([item["schema"] for item in coverage["types"]], ["Stable.json"])
            self.assertEqual([item["path"] for item in coverage["fields"]], ["Stable.json#/properties/kept"])
            self.assertEqual([item["method"] for item in manifest["entries"]], ["stable/read"])

    def test_update_manifest_replaces_surface_and_records_mechanical_source(self) -> None:
        with tempfile.TemporaryDirectory() as raw_tmp:
            path = Path(raw_tmp) / "manifest.json"
            write_json(path, {"schema_version": 1, "classification_sources": {}, "surface": []})
            surface = [{"kind": "type", "name": "Event", "signature": "struct{}", "stability": "stable"}]

            classified_manifest.update_manifest(path, surface)

            manifest = classified_manifest.load_json(path)
            self.assertEqual(manifest["schema_version"], 2)
            self.assertEqual(manifest["surface"], surface)
            self.assertIn("without and with experimental", manifest["classification_sources"]["generated_surface"])


if __name__ == "__main__":
    unittest.main()

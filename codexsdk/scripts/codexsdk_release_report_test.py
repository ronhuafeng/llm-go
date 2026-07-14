#!/usr/bin/env python3

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))

import codexsdk_release_report as release_report


def manifest(*entries: tuple[str, str, str]) -> dict[str, object]:
    return {
        "surface": [
            {"kind": kind, "name": name, "signature": f"{kind}:{name}", "stability": stability}
            for kind, name, stability in entries
        ]
    }


def facade_manifest(*operations: str) -> dict[str, object]:
    value = manifest(("type", "Event", "stable"))
    value["entries"] = [
        {
            "direction": "client_to_server",
            "kind": "request",
            "method": f"model/{operation.lower()}",
            "facade_target": f"Models().{operation}",
            "facade_status": "generated",
            "params_or_payload_schema": "ModelListParams",
            "response_type": f"Model{operation}Response",
            "stability": "stable",
        }
        for operation in operations
    ]
    return value


class ReleaseReportTest(unittest.TestCase):
    def test_experimental_removal_is_incompatible_and_classified(self) -> None:
        base = manifest(
            ("type", "Event", "mixed"),
            ("field", "Event.ID", "stable"),
            ("field", "Event.Preview", "experimental"),
        )
        target = manifest(
            ("type", "Event", "stable"),
            ("field", "Event.ID", "stable"),
        )

        report = release_report.compatibility_report(base, target)

        self.assertEqual(report["compatibility_impact"], "incompatible")
        self.assertEqual(report["removed"][0]["name"], "Event.Preview")
        self.assertEqual(report["removed"][0]["classification"], "experimental")
        self.assertEqual(report["counts_by_classification"]["experimental"]["removed"], 1)
        self.assertEqual(report["reclassified"][0], {"kind": "type", "name": "Event", "from": "mixed", "to": "stable"})

    def test_additive_surface_is_not_incompatible(self) -> None:
        base = manifest(("type", "Event", "stable"))
        target = manifest(
            ("type", "Event", "stable"),
            ("type", "Preview", "experimental"),
        )

        report = release_report.compatibility_report(base, target)

        self.assertEqual(report["compatibility_impact"], "additive_or_metadata_only")
        self.assertEqual(report["counts_by_classification"]["experimental"]["added"], 1)

    def test_signature_change_is_incompatible_and_classified(self) -> None:
        base = manifest(("field", "Event.ID", "experimental"))
        target = manifest(("field", "Event.ID", "experimental"))
        target["surface"][0]["signature"] = "int64"

        report = release_report.compatibility_report(base, target)

        self.assertEqual(report["compatibility_impact"], "incompatible")
        self.assertEqual(report["changed"][0]["name"], "Event.ID")
        self.assertEqual(report["changed"][0]["classification"], "experimental")

    def test_concrete_method_growth_is_additive_without_implementation_obligation(self) -> None:
        base = facade_manifest("List")
        target = facade_manifest("List", "Read")

        report = release_report.compatibility_report(base, target)

        self.assertEqual(report["compatibility_impact"], "additive_or_metadata_only")
        self.assertEqual(report["external_implementation_obligations"], [])
        self.assertEqual(
            [(item["kind"], item["name"]) for item in report["added"]],
            [("method", "codexsdk.Models.Read")],
        )

    def test_interface_method_growth_is_an_external_implementation_obligation(self) -> None:
        base = manifest(
            ("type", "Models", "stable"),
            ("interface_method", "Models.List", "stable"),
        )
        target = manifest(
            ("type", "Models", "stable"),
            ("interface_method", "Models.List", "stable"),
            ("interface_method", "Models.Read", "stable"),
        )

        report = release_report.compatibility_report(base, target)

        self.assertEqual(report["compatibility_impact"], "incompatible")
        self.assertEqual(
            [item["name"] for item in report["external_implementation_obligations"]],
            ["Models.Read"],
        )


if __name__ == "__main__":
    unittest.main()

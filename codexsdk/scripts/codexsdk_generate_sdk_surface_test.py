#!/usr/bin/env python3

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))

import codexsdk_generate_sdk_surface as sdk_surface


class SDKSurfaceReportTest(unittest.TestCase):
    def test_compatibility_surface_lists_generated_facades_only(self) -> None:
        manifest = {
            "entries": [
                {
                    "direction": "client_to_server",
                    "kind": "request",
                    "facade_target": "Models().List",
                    "facade_status": "generated",
                    "params_or_payload_schema": "ModelListParams",
                    "response_type": "ModelListResponse",
                    "stability": "stable",
                },
                {
                    "direction": "client_to_server",
                    "kind": "request",
                    "facade_target": "Models().Read",
                    "facade_status": "deferred_missing_generated_types",
                    "params_or_payload_schema": "ModelReadParams",
                    "response_type": "ModelReadResponse",
                    "stability": "experimental",
                },
            ]
        }
        surface = sdk_surface.facade_compatibility_surface(manifest)
        names = {entry["name"] for entry in surface}
        self.assertIn("codexsdk.Models", names)
        self.assertIn("codexsdk.Models.List", names)
        self.assertNotIn("codexsdk.Models.Read", names)

    def test_module_cannot_write_sdk_source(self) -> None:
        self.assertFalse(hasattr(sdk_surface, "render"))
        self.assertFalse(hasattr(sdk_surface, "surface_methods"))
        self.assertFalse(hasattr(sdk_surface, "main"))


if __name__ == "__main__":
    unittest.main()

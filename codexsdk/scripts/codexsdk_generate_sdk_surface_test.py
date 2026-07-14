#!/usr/bin/env python3

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))

import codexsdk_generate_sdk_surface as sdk_surface


class SDKSurfaceGeneratorTest(unittest.TestCase):
    def test_facade_is_exported_concrete_opaque_value(self) -> None:
        generated = sdk_surface.render(
            [
                sdk_surface.SurfaceMethod(
                    accessor="Models",
                    operation="List",
                    method="model/list",
                    method_const="MethodModelList",
                    params_type="ModelListParams",
                    response_type="ModelListResponse",
                    stability="stable",
                ),
                sdk_surface.SurfaceMethod(
                    accessor="Models",
                    operation="Read",
                    method="model/read",
                    method_const="MethodModelRead",
                    params_type="ModelReadParams",
                    response_type="ModelReadResponse",
                    stability="stable",
                ),
            ]
        )

        self.assertIn(
            "// Models is an opaque generated facade for exact Codex operations.\n"
            "type Models struct {\n\tclient *Client\n}",
            generated,
        )
        self.assertNotIn("type Models interface", generated)
        self.assertIn("func (c *Client) Models() Models", generated)
        self.assertIn("func (f Models) List(", generated)
        self.assertIn("func (f Models) Read(", generated)

    def test_surface_methods_fail_closed_for_missing_generated_prerequisite(self) -> None:
        manifest = {
            "entries": [
                {
                    "direction": "client_to_server",
                    "kind": "request",
                    "facade_target": "Models().List",
                    "facade_status": "generated",
                    "method": "model/list",
                    "params_or_payload_schema": "ModelListParams",
                    "response_type": "ModelListResponse",
                    "stability": "stable",
                }
            ]
        }
        with self.assertRaisesRegex(SystemExit, "missing response type ModelListResponse"):
            sdk_surface.surface_methods(
                manifest,
                {"model/list": "MethodModelList"},
                {"ModelListParams"},
            )

    def test_surface_methods_require_explicit_status_and_valid_target(self) -> None:
        entry = {
            "direction": "client_to_server",
            "kind": "request",
            "facade_target": "Models().List",
            "method": "model/list",
            "params_or_payload_schema": "ModelListParams",
            "response_type": "ModelListResponse",
            "stability": "stable",
        }
        with self.assertRaisesRegex(SystemExit, "invalid or missing facade_status"):
            sdk_surface.surface_methods(
                {"entries": [entry]},
                {"model/list": "MethodModelList"},
                {"ModelListParams", "ModelListResponse"},
            )
        entry["facade_status"] = "generated"
        entry["facade_target"] = "Models.List"
        with self.assertRaisesRegex(SystemExit, "invalid generated facade target"):
            sdk_surface.surface_methods(
                {"entries": [entry]},
                {"model/list": "MethodModelList"},
                {"ModelListParams", "ModelListResponse"},
            )

    def test_surface_methods_allow_explicit_internal_request_target(self) -> None:
        manifest = {
            "entries": [
                {
                    "direction": "client_to_server",
                    "kind": "request",
                    "facade_target": "internal.InitializeHandshake.Request",
                    "method": "initialize",
                }
            ]
        }

        self.assertEqual(sdk_surface.surface_methods(manifest, {}, set()), [])

    def test_deferred_facade_requires_a_missing_prerequisite(self) -> None:
        entry = {
            "direction": "client_to_server",
            "kind": "request",
            "facade_target": "Models().Read",
            "facade_status": "deferred_missing_generated_types",
            "method": "model/read",
            "params_or_payload_schema": "ModelReadParams",
            "response_type": "ModelReadResponse",
            "stability": "experimental",
        }
        self.assertEqual(
            sdk_surface.surface_methods(
                {"entries": [entry]},
                {"model/read": "MethodModelRead"},
                {"ModelReadParams"},
            ),
            [],
        )
        with self.assertRaisesRegex(SystemExit, "has all generated prerequisites"):
            sdk_surface.surface_methods(
                {"entries": [entry]},
                {"model/read": "MethodModelRead"},
                {"ModelReadParams", "ModelReadResponse"},
            )


if __name__ == "__main__":
    unittest.main()

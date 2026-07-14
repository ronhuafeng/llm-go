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

import codexsdk_resolve_upstream as resolve


SHA1 = "1" * 40
SHA2 = "2" * 40
SHA3 = "3" * 40
SHA4 = "4" * 40


class ResolveUpstreamTest(unittest.TestCase):
    def test_latest_stable_tag_uses_semver_order(self) -> None:
        output = "\n".join(
            [
                f"{SHA1}\trefs/tags/rust-v0.99.0",
                f"{SHA2}\trefs/tags/rust-v0.100.0",
                f"{SHA3}\trefs/tags/not-rust-v9.0.0",
            ]
        )
        self.assertEqual(resolve.latest_stable_rust_tag(output), "rust-v0.100.0")

    def test_latest_stable_tag_ignores_prerelease_and_peeled_refs(self) -> None:
        output = "\n".join(
            [
                f"{SHA1}\trefs/tags/rust-v0.99.0",
                f"{SHA2}\trefs/tags/rust-v0.100.0-alpha.1",
                f"{SHA3}\trefs/tags/rust-v0.100.0-alpha.1^{{}}",
                f"{SHA4}\trefs/tags/rust-v0.100.0^{{}}",
            ]
        )
        self.assertEqual(resolve.latest_stable_rust_tag(output), "rust-v0.99.0")

    def test_infers_target_kinds(self) -> None:
        self.assertEqual(resolve.infer_ref_kind("rust-v0.100.0"), "stable_rust_tag")
        self.assertEqual(resolve.infer_ref_kind(SHA1), "manual_commit")
        self.assertEqual(resolve.infer_ref_kind("main"), "manual_ref")

    def test_explicit_sha_resolves_without_remote_lookup(self) -> None:
        target = resolve.resolve_upstream("unused", SHA1)
        self.assertEqual(target.upstream_ref, SHA1)
        self.assertEqual(target.upstream_ref_kind, "manual_commit")
        self.assertEqual(target.upstream_sha, SHA1)
        self.assertEqual(target.tag_sha, "")
        self.assertEqual(target.peeled_commit_sha, SHA1)
        self.assertTrue(target.target_explicit)

    def test_explicit_tag_records_tag_and_peeled_sha(self) -> None:
        script = Path(__file__).with_name("codexsdk_resolve_upstream.py")
        with tempfile.TemporaryDirectory() as tmp:
            fake_git = Path(tmp) / "git"
            fake_git.write_text(
                "\n".join(
                    [
                        "#!/usr/bin/env python3",
                        "import sys",
                        "args = sys.argv[1:]",
                        "if args[:2] != ['ls-remote', 'fake']:",
                        "    raise SystemExit(f'unexpected args: {args!r}')",
                        "patterns = set(args[2:])",
                        "if 'refs/tags/rust-v0.100.0' in patterns:",
                        f"    print('{SHA1}\\trefs/tags/rust-v0.100.0')",
                        "if 'refs/tags/rust-v0.100.0^{}' in patterns:",
                        f"    print('{SHA2}\\trefs/tags/rust-v0.100.0^{{}}')",
                    ]
                ),
                encoding="utf-8",
            )
            fake_git.chmod(0o755)
            env = os.environ.copy()
            env["PATH"] = f"{tmp}{os.pathsep}{env['PATH']}"
            completed = subprocess.run(
                [
                    sys.executable,
                    str(script),
                    "--remote",
                    "fake",
                    "--upstream-ref",
                    "rust-v0.100.0",
                    "--json",
                ],
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                env=env,
            )
        payload = json.loads(completed.stdout)
        self.assertEqual(payload["ref_name"], "rust-v0.100.0")
        self.assertEqual(payload["ref_kind"], "stable_rust_tag")
        self.assertEqual(payload["tag_sha"], SHA1)
        self.assertEqual(payload["peeled_commit_sha"], SHA2)
        self.assertEqual(payload["upstream_sha"], SHA2)
        self.assertTrue(payload["target_explicit"])
        self.assertEqual(completed.stderr, "")

    def test_cli_json_prints_target_metadata(self) -> None:
        script = Path(__file__).with_name("codexsdk_resolve_upstream.py")
        with tempfile.TemporaryDirectory() as tmp:
            fake_git = Path(tmp) / "git"
            fake_git.write_text(
                "\n".join(
                    [
                        "#!/usr/bin/env python3",
                        "import sys",
                        "args = sys.argv[1:]",
                        "if args[:2] != ['ls-remote', 'fake']:",
                        "    raise SystemExit(f'unexpected args: {args!r}')",
                        "patterns = set(args[2:])",
                        "if 'refs/tags/rust-v*' in patterns:",
                        f"    print('{SHA1}\\trefs/tags/rust-v0.99.0')",
                        f"    print('{SHA2}\\trefs/tags/rust-v0.100.0')",
                        "if 'refs/tags/rust-v0.100.0' in patterns:",
                        f"    print('{SHA2}\\trefs/tags/rust-v0.100.0')",
                        "",
                    ]
                ),
                encoding="utf-8",
            )
            fake_git.chmod(0o755)
            env = os.environ.copy()
            env["PATH"] = f"{tmp}{os.pathsep}{env['PATH']}"
            completed = subprocess.run(
                [sys.executable, str(script), "--remote", "fake", "--json"],
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                env=env,
            )
        payload = json.loads(completed.stdout)
        self.assertEqual(payload["ref_name"], "rust-v0.100.0")
        self.assertEqual(payload["ref_kind"], "stable_rust_tag")
        self.assertEqual(payload["tag_sha"], SHA2)
        self.assertEqual(payload["peeled_commit_sha"], SHA2)
        self.assertEqual(payload["upstream_ref"], "rust-v0.100.0")
        self.assertEqual(payload["upstream_ref_kind"], "stable_rust_tag")
        self.assertEqual(payload["upstream_sha"], SHA2)
        self.assertFalse(payload["target_explicit"])
        self.assertEqual(completed.stderr, "")


if __name__ == "__main__":
    unittest.main()

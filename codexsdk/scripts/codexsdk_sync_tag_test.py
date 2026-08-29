#!/usr/bin/env python3

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.dirname(__file__))

import codexsdk_sync_tag as sync_tag


UPSTREAM_SHA = "c73296a000000000000000000000000000000000"
SDK_SHA = "1" * 40


class SyncTagTest(unittest.TestCase):
    def test_stable_rust_tag_uses_upstream_namespace(self):
        metadata = {
            "source_ref_kind": "stable_rust_tag",
            "source_ref_name": "rust-v0.140.0",
            "source_commit": UPSTREAM_SHA,
        }
        self.assertEqual(sync_tag.tag_name(metadata), "upstream-codex-rust-v0.140.0")

    def test_manual_commit_has_no_fallback_tag(self):
        metadata = {
            "source_ref_kind": "manual_commit",
            "source_ref_name": UPSTREAM_SHA,
            "source_commit": UPSTREAM_SHA,
        }
        with self.assertRaisesRegex(ValueError, "stable_rust_tag"):
            sync_tag.tag_name(metadata)

    def test_manual_ref_has_no_fallback_tag(self):
        metadata = {
            "source_ref_kind": "manual_ref",
            "source_ref_name": "refs/heads/main",
            "source_commit": UPSTREAM_SHA,
        }
        with self.assertRaisesRegex(ValueError, "stable_rust_tag"):
            sync_tag.tag_name(metadata)

    def test_existing_base_tag_blocks(self):
        choice = sync_tag.choose_tag(
            "upstream-codex-rust-v0.140.0",
            SDK_SHA,
            "2" * 40,
        )
        self.assertEqual(choice.action, "block")

    def test_tag_message_includes_upstream_and_sdk_commits(self):
        metadata = {
            "source_repo": "https://github.com/openai/codex",
            "source_ref_kind": "stable_rust_tag",
            "source_ref_name": "rust-v0.140.0",
            "source_commit": UPSTREAM_SHA,
            "schema_bundle_sha256": "a" * 64,
            "codex_version": "codex-cli 0.1.0",
        }
        message = sync_tag.sync_tag_message(metadata, SDK_SHA)
        self.assertTrue(message.startswith("Sync llm-go/codexsdk with openai/codex rust-v0.140.0\n"))
        self.assertIn("upstream_ref: rust-v0.140.0", message)
        self.assertIn(f"upstream_commit: {UPSTREAM_SHA}", message)
        self.assertIn(f"codexsdk_commit: {SDK_SHA}", message)

    def test_cli_is_quiet_without_json(self):
        with sync_tag_repo() as repo:
            completed = run_sync_tag(repo)
            self.assertEqual(completed.stdout, "")
            self.assertEqual(completed.stderr, "")

    def test_cli_json_prints_machine_payload(self):
        with sync_tag_repo() as repo:
            completed = run_sync_tag(repo, "--json")
            payload = json.loads(completed.stdout)
            self.assertEqual(payload["action"], "create")
            self.assertEqual(payload["tag_name"], "upstream-codex-rust-v0.140.0")
            self.assertEqual(payload["upstream_commit"], UPSTREAM_SHA)
            self.assertEqual(completed.stderr, "")

    def test_cli_reads_metadata_from_module_subdirectory(self):
        with sync_tag_repo(module_dir="codexsdk") as module:
            completed = run_sync_tag(module, "--json")
            payload = json.loads(completed.stdout)
            self.assertEqual(payload["tag_name"], "upstream-codex-rust-v0.140.0")
            self.assertEqual(payload["upstream_commit"], UPSTREAM_SHA)

    def test_cli_reads_metadata_from_repo_root_codexsdk_path(self):
        with sync_tag_repo(module_dir="codexsdk") as module:
            completed = run_sync_tag(module.parent, "--json")
            payload = json.loads(completed.stdout)
            self.assertEqual(payload["tag_name"], "upstream-codex-rust-v0.140.0")
            self.assertEqual(payload["upstream_commit"], UPSTREAM_SHA)

    def test_cli_reports_raw_git_error_with_command_context(self):
        with sync_tag_repo(module_dir="codexsdk") as module:
            subprocess.run(["git", "rm", sync_tag.METADATA_PATH], cwd=module, check=True, stdout=subprocess.PIPE)
            subprocess.run(["git", "commit", "-q", "-m", "remove metadata"], cwd=module, check=True)

            completed = run_sync_tag(module, "--json", check=False)

            self.assertNotEqual(completed.returncode, 0)
            self.assertIn("fatal:", completed.stderr)
            self.assertIn("baseline_metadata.json", completed.stderr)
            self.assertIn("exit code 128", completed.stderr)

    def test_cli_rejects_fallback_tag_option(self):
        with sync_tag_repo() as repo:
            completed = run_sync_tag(repo, "--next-suffix", check=False)

            self.assertNotEqual(completed.returncode, 0)
            self.assertIn("--next-suffix", completed.stderr)

    def test_cli_dry_run_block_is_quiet_without_json(self):
        with sync_tag_repo() as repo:
            subprocess.run(
                ["git", "tag", "-a", "upstream-codex-rust-v0.140.0", "HEAD", "-m", "old"],
                cwd=repo,
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            subprocess.run(["git", "commit", "--allow-empty", "-q", "-m", "follow-up"], cwd=repo, check=True)
            completed = run_sync_tag(repo)
            self.assertEqual(completed.stdout, "")
            self.assertEqual(completed.stderr, "")

    def test_cli_tag_collision_fails_without_fallback_and_reports_commits(self):
        base_tag = "upstream-codex-rust-v0.140.0"
        with sync_tag_repo() as repo:
            existing_commit = subprocess.run(
                ["git", "rev-parse", "HEAD"], cwd=repo, check=True, stdout=subprocess.PIPE, text=True
            ).stdout.strip()
            subprocess.run(
                ["git", "tag", "-a", base_tag, "HEAD", "-m", "old"],
                cwd=repo,
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            subprocess.run(["git", "commit", "--allow-empty", "-q", "-m", "follow-up"], cwd=repo, check=True)
            head_commit = subprocess.run(
                ["git", "rev-parse", "HEAD"], cwd=repo, check=True, stdout=subprocess.PIPE, text=True
            ).stdout.strip()

            completed = run_sync_tag(repo, "--create", "--json", check=False)

            self.assertEqual(completed.returncode, 1)
            self.assertIn(base_tag, completed.stderr)
            self.assertIn(existing_commit, completed.stderr)
            self.assertIn(head_commit, completed.stderr)
            tags = subprocess.run(
                ["git", "tag", "--list", f"{base_tag}*"], cwd=repo, check=True, stdout=subprocess.PIPE, text=True
            ).stdout.splitlines()
            self.assertEqual(tags, [base_tag])

class sync_tag_repo:
    def __init__(self, module_dir=""):
        self.module_dir = module_dir

    def __enter__(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.path = self.root / self.module_dir
        self.path.mkdir(parents=True, exist_ok=True)
        subprocess.run(["git", "init", "-q"], cwd=self.root, check=True)
        subprocess.run(["git", "config", "user.email", "codex@example.com"], cwd=self.root, check=True)
        subprocess.run(["git", "config", "user.name", "Codex"], cwd=self.root, check=True)
        metadata_path = self.path / sync_tag.METADATA_PATH
        metadata_path.parent.mkdir(parents=True, exist_ok=True)
        metadata_path.write_text(
            json.dumps(
                {
                    "codex_version": "codex-cli 0.1.0",
                    "schema_bundle_sha256": "a" * 64,
                    "source_commit": UPSTREAM_SHA,
                    "source_ref_kind": "stable_rust_tag",
                    "source_ref_name": "rust-v0.140.0",
                    "source_repo": "https://github.com/openai/codex",
                }
            )
            + "\n",
            encoding="utf-8",
        )
        subprocess.run(["git", "add", "."], cwd=self.root, check=True)
        subprocess.run(["git", "commit", "-q", "-m", "baseline"], cwd=self.root, check=True)
        return self.path

    def __exit__(self, exc_type, exc, tb):
        self.tmp.cleanup()


def run_sync_tag(repo: Path, *args: str, check=True) -> subprocess.CompletedProcess[str]:
    script = Path(__file__).with_name("codexsdk_sync_tag.py")
    return subprocess.run(
        [sys.executable, str(script), *args],
        cwd=repo,
        check=check,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


if __name__ == "__main__":
    unittest.main()

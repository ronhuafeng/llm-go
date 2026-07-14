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

    def test_existing_base_tag_blocks_without_suffix(self):
        choice = sync_tag.choose_tag(
            "upstream-codex-rust-v0.140.0",
            {"upstream-codex-rust-v0.140.0": SDK_SHA},
            "2" * 40,
            next_suffix=False,
        )
        self.assertEqual(choice.action, "block")

    def test_existing_base_tag_uses_next_suffix(self):
        choice = sync_tag.choose_tag(
            "upstream-codex-rust-v0.140.0",
            {"upstream-codex-rust-v0.140.0": SDK_SHA},
            "2" * 40,
            next_suffix=True,
        )
        self.assertEqual(choice.action, "create")
        self.assertEqual(choice.tag_name, "upstream-codex-rust-v0.140.0-sync.2")

    def test_existing_follow_up_tag_reuses_current_commit(self):
        choice = sync_tag.choose_tag(
            "upstream-codex-rust-v0.140.0",
            {
                "upstream-codex-rust-v0.140.0": SDK_SHA,
                "upstream-codex-rust-v0.140.0-sync.2": "2" * 40,
            },
            "2" * 40,
            next_suffix=True,
        )
        self.assertEqual(choice.action, "exists")
        self.assertEqual(choice.tag_name, "upstream-codex-rust-v0.140.0-sync.2")

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

    def test_cli_next_suffix_uses_remote_tags_when_local_tags_are_stale(self):
        with sync_tag_repo() as repo, tempfile.TemporaryDirectory() as remote_tmp:
            remote = Path(remote_tmp) / "origin.git"
            subprocess.run(["git", "init", "--bare", "-q", str(remote)], check=True)
            subprocess.run(["git", "remote", "add", "origin", str(remote)], cwd=repo, check=True)
            subprocess.run(
                ["git", "tag", "-a", "upstream-codex-rust-v0.140.0", "HEAD", "-m", "old"],
                cwd=repo,
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            subprocess.run(
                ["git", "push", "-q", "origin", "refs/tags/upstream-codex-rust-v0.140.0"],
                cwd=repo,
                check=True,
            )
            subprocess.run(
                ["git", "tag", "-d", "upstream-codex-rust-v0.140.0"],
                cwd=repo,
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            subprocess.run(["git", "commit", "--allow-empty", "-q", "-m", "follow-up"], cwd=repo, check=True)

            completed = run_sync_tag(repo, "--next-suffix", "--create", "--push", "origin", "--json")
            payload = json.loads(completed.stdout)

            self.assertEqual(payload["action"], "created")
            self.assertEqual(payload["tag_name"], "upstream-codex-rust-v0.140.0-sync.2")
            self.assertEqual(payload["pushed_remote"], "origin")

            head = subprocess.run(
                ["git", "rev-parse", "--verify", "HEAD^{commit}"],
                cwd=repo,
                check=True,
                stdout=subprocess.PIPE,
                text=True,
            ).stdout.strip()
            remote_tag = subprocess.run(
                [
                    "git",
                    "ls-remote",
                    "--tags",
                    "origin",
                    "refs/tags/upstream-codex-rust-v0.140.0-sync.2^{}",
                    "refs/tags/upstream-codex-rust-v0.140.0-sync.2",
                ],
                cwd=repo,
                check=True,
                stdout=subprocess.PIPE,
                text=True,
            ).stdout
            self.assertIn(head, remote_tag)


class sync_tag_repo:
    def __enter__(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.path = Path(self.tmp.name)
        subprocess.run(["git", "init", "-q"], cwd=self.path, check=True)
        subprocess.run(["git", "config", "user.email", "codex@example.com"], cwd=self.path, check=True)
        subprocess.run(["git", "config", "user.name", "Codex"], cwd=self.path, check=True)
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
        subprocess.run(["git", "add", sync_tag.METADATA_PATH], cwd=self.path, check=True)
        subprocess.run(["git", "commit", "-q", "-m", "baseline"], cwd=self.path, check=True)
        return self.path

    def __exit__(self, exc_type, exc, tb):
        self.tmp.cleanup()


def run_sync_tag(repo: Path, *args: str) -> subprocess.CompletedProcess[str]:
    script = Path(__file__).with_name("codexsdk_sync_tag.py")
    return subprocess.run(
        [sys.executable, str(script), *args],
        cwd=repo,
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


if __name__ == "__main__":
    unittest.main()

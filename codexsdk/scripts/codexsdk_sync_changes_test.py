#!/usr/bin/env python3

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("codexsdk_sync_changes.py")


def git(repo: Path, *args: str) -> str:
    return subprocess.check_output(["git", "-C", str(repo), *args], text=True).strip()


def init_repo(root: Path) -> None:
    subprocess.run(["git", "init", "-q", str(root)], check=True)
    git(root, "config", "user.email", "codex@example.com")
    git(root, "config", "user.name", "Codex")
    tracked = root / "codexsdk/internal/protocolschema/appserver/v2/ClientRequest.json"
    tracked.parent.mkdir(parents=True)
    tracked.write_text("{}\n", encoding="utf-8")
    git(root, "add", ".")
    git(root, "commit", "-q", "-m", "baseline")


class SyncChangesTest(unittest.TestCase):
    def test_assert_clean_rejects_pre_existing_untracked_changes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            init_repo(repo)
            (repo / "codexsdk/user-work.go").write_text("package codexsdk\n", encoding="utf-8")

            completed = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "assert-clean",
                    "--repo-root",
                    str(repo),
                ],
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            self.assertNotEqual(completed.returncode, 0)
            self.assertIn("sync worktree must be clean before apply", completed.stderr)

    def test_capture_includes_untracked_generated_schema(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            init_repo(repo)
            added = repo / "codexsdk/internal/protocolschema/appserver/v2/NewRequest.json"
            added.write_text("{}\n", encoding="utf-8")
            manifest = repo / "manifest.json"

            subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "capture",
                    "--repo-root",
                    str(repo),
                    "--phase",
                    "mechanical",
                    "--output",
                    str(manifest),
                ],
                check=True,
            )

            payload = json.loads(manifest.read_text(encoding="utf-8"))
            self.assertEqual(
                payload["paths"],
                ["codexsdk/internal/protocolschema/appserver/v2/NewRequest.json"],
            )

    def test_stage_rejects_changes_added_after_manifest_capture(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            init_repo(repo)
            generated = repo / "codexsdk/protocolv2/client.gen.go"
            generated.parent.mkdir(parents=True)
            generated.write_text("package protocolv2\n", encoding="utf-8")
            manifest = Path(tmp).parent / f"{repo.name}-manifest.json"
            self.addCleanup(manifest.unlink, missing_ok=True)
            subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "capture",
                    "--repo-root",
                    str(repo),
                    "--phase",
                    "final",
                    "--output",
                    str(manifest),
                ],
                check=True,
            )
            (repo / "codexsdk/late.go").write_text("package codexsdk\n", encoding="utf-8")

            completed = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "stage",
                    "--repo-root",
                    str(repo),
                    "--manifest",
                    str(manifest),
                ],
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            self.assertNotEqual(completed.returncode, 0)
            self.assertIn("changed since the manifest was captured", completed.stderr)
            self.assertEqual(git(repo, "diff", "--cached", "--name-only"), "")

    def test_final_capture_rejects_scope_external_changes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            init_repo(repo)
            (repo / ".github").mkdir()
            (repo / ".github/workflow.yml").write_text("name: changed\n", encoding="utf-8")

            completed = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "capture",
                    "--repo-root",
                    str(repo),
                    "--phase",
                    "final",
                    "--output",
                    str(Path(tmp).parent / f"{repo.name}-external.json"),
                ],
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            self.assertNotEqual(completed.returncode, 0)
            self.assertIn("sync changes escape the final scope", completed.stderr)

    def test_stage_adds_exact_manifest_including_untracked_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            init_repo(repo)
            generated = repo / "codexsdk/protocolv2/client.gen.go"
            generated.parent.mkdir(parents=True)
            generated.write_text("package protocolv2\n", encoding="utf-8")
            manifest = Path(tmp).parent / f"{repo.name}-stage.json"
            self.addCleanup(manifest.unlink, missing_ok=True)
            subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "capture",
                    "--repo-root",
                    str(repo),
                    "--phase",
                    "final",
                    "--output",
                    str(manifest),
                ],
                check=True,
            )

            subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "stage",
                    "--repo-root",
                    str(repo),
                    "--manifest",
                    str(manifest),
                ],
                check=True,
            )

            self.assertEqual(git(repo, "diff", "--cached", "--name-only"), "codexsdk/protocolv2/client.gen.go")


if __name__ == "__main__":
    unittest.main()

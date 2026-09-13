#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("codexsdk_publish_sync_pr.sh")
TARGET_SHA = "a" * 40
OTHER_SHA = "b" * 40


def run(*args: str, cwd: Path | None = None, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        list(args),
        cwd=cwd,
        env=env,
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


def pr_body(*, target_ref: str, target_kind: str, target_sha: str, sync_commit: str, base_branch: str = "main") -> str:
    return textwrap.dedent(
        f"""\
        <!-- codexsdk-upstream-sync
        upstream_ref: {target_ref}
        upstream_ref_kind: {target_kind}
        upstream_commit: {target_sha}
        sync_commit: {sync_commit}
        base_branch: {base_branch}
        -->
        """
    )


class PublishSyncPrTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name) / "repo"
        self.bare = Path(self.tmp.name) / "origin.git"
        self.scripts = self.root / "codexsdk/scripts"
        self.fake_bin = Path(self.tmp.name) / "bin"
        self.pr_state = Path(self.tmp.name) / "pr-state.json"
        self.scripts.mkdir(parents=True)
        self.fake_bin.mkdir()
        shutil.copy2(SCRIPT, self.scripts / SCRIPT.name)
        (self.scripts / "codexsdk_resolve_upstream.py").write_text(
            f"#!/usr/bin/env python3\nprint('{{\"peeled_commit_sha\": \"{TARGET_SHA}\"}}')\n",
            encoding="utf-8",
        )
        self.pr_state.write_text("[]\n", encoding="utf-8")
        (self.fake_bin / "gh").write_text(
            textwrap.dedent(
                f"""\
                #!/usr/bin/env bash
                set -euo pipefail
                state={self.pr_state}
                if [[ "$1 $2" == "pr list" ]]; then
                  cat "${{state}}"
                  exit 0
                fi
                if [[ "$1 $2" == "pr create" ]]; then
                  echo "https://github.example/pull/1"
                  exit 0
                fi
                if [[ "$1 $2" == "pr view" ]]; then
                  echo "1"
                  exit 0
                fi
                if [[ "$1 $2" == "pr edit" ]]; then
                  exit 0
                fi
                echo "unexpected gh args: $*" >&2
                exit 1
                """
            ),
            encoding="utf-8",
        )
        for executable in (
            self.scripts / SCRIPT.name,
            self.scripts / "codexsdk_resolve_upstream.py",
            self.fake_bin / "gh",
        ):
            executable.chmod(0o755)

        run("git", "init", "-q", "--bare", str(self.bare))
        run("git", "init", "-q", "-b", "main", str(self.root))
        run("git", "config", "user.email", "codex@example.com", cwd=self.root)
        run("git", "config", "user.name", "Codex", cwd=self.root)
        run("git", "add", ".", cwd=self.root)
        run("git", "commit", "-q", "-m", "baseline", cwd=self.root)
        run("git", "remote", "add", "origin", str(self.bare), cwd=self.root)
        run("git", "push", "-q", "-u", "origin", "main", cwd=self.root)
        (self.root / "codexsdk/sync.txt").write_text("sync\n", encoding="utf-8")
        run("git", "add", "codexsdk/sync.txt", cwd=self.root)
        run("git", "commit", "-q", "-m", "sync", cwd=self.root)
        self.validated_commit = run("git", "rev-parse", "HEAD", cwd=self.root).stdout.strip()
        self.output = Path(self.tmp.name) / "github-output.txt"
        self.env = {
            **os.environ,
            "GITHUB_OUTPUT": str(self.output),
            "PATH": f"{self.fake_bin}:{os.environ['PATH']}",
        }

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def publish(self, *extra: str, commit: str | None = None) -> subprocess.CompletedProcess[str]:
        args = [
            str(self.scripts / SCRIPT.name),
            "--land-ref",
            "main",
            "--default-branch",
            "main",
            "--target-ref",
            TARGET_SHA,
            "--target-kind",
            "manual_commit",
            "--target-sha",
            TARGET_SHA,
            "--validated-commit",
            commit or self.validated_commit,
            *extra,
        ]
        return subprocess.run(
            args,
            cwd=self.root / "codexsdk",
            env=self.env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

    def test_publish_exact_tree_when_landing_ref_unchanged(self) -> None:
        result = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        github_output = self.output.read_text(encoding="utf-8")
        self.assertIn(f"-{TARGET_SHA[:12]}\n", github_output)
        sync_branch = next(
            line.removeprefix("sync_branch=")
            for line in github_output.splitlines()
            if line.startswith("sync_branch=")
        )
        published = run("git", "--git-dir", str(self.bare), "rev-parse", f"refs/heads/{sync_branch}").stdout.strip()
        self.assertEqual(published, self.validated_commit)

    def test_publish_reuses_validation_and_refuses_divergent_branch_retry(self) -> None:
        first = self.publish()
        self.assertEqual(first.returncode, 0, first.stderr)
        github_output = self.output.read_text(encoding="utf-8")
        sync_branch = next(
            line.removeprefix("sync_branch=")
            for line in github_output.splitlines()
            if line.startswith("sync_branch=")
        )
        first_remote_commit = run(
            "git",
            "--git-dir",
            str(self.bare),
            "rev-parse",
            f"refs/heads/{sync_branch}",
        ).stdout.strip()
        run("git", "reset", "--hard", "-q", "origin/main", cwd=self.root)
        (self.root / "codexsdk/sync.txt").write_text("different retry\n", encoding="utf-8")
        run("git", "add", "codexsdk/sync.txt", cwd=self.root)
        run("git", "commit", "-q", "-m", "different retry", cwd=self.root)
        retry_commit = run("git", "rev-parse", "HEAD", cwd=self.root).stdout.strip()
        retry = self.publish(commit=retry_commit)
        self.assertNotEqual(retry.returncode, 0)
        self.assertIn("Refusing to overwrite existing sync branch", retry.stderr)
        self.assertEqual(
            run("git", "--git-dir", str(self.bare), "rev-parse", f"refs/heads/{sync_branch}").stdout.strip(),
            first_remote_commit,
        )

    def test_landing_ref_movement_fails_closed_without_rebase(self) -> None:
        parent = run("git", "rev-parse", f"{self.validated_commit}^", cwd=self.root).stdout.strip()
        work = Path(self.tmp.name) / "main-advance"
        run("git", "clone", "-q", str(self.bare), str(work))
        run("git", "config", "user.email", "codex@example.com", cwd=work)
        run("git", "config", "user.name", "Codex", cwd=work)
        run("git", "checkout", "-q", "-B", "main", parent, cwd=work)
        (work / "unrelated.txt").write_text("moved\n", encoding="utf-8")
        run("git", "add", "unrelated.txt", cwd=work)
        run("git", "commit", "-q", "-m", "advance main", cwd=work)
        push = subprocess.run(
            ["git", "push", "-q", "origin", "HEAD:main"],
            cwd=work,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        if push.returncode != 0:
            bare_main = run("git", "--git-dir", str(self.bare), "rev-parse", "refs/heads/main").stdout.strip()
            work_head = run("git", "rev-parse", "HEAD", cwd=work).stdout.strip()
            self.fail(f"advance main failed: {push.stderr} bare={bare_main} work={work_head} parent={parent}")

        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("landing ref main moved", result.stderr)
        self.assertNotIn("git rebase", Path(self.scripts / SCRIPT.name).read_text(encoding="utf-8"))
        branches = run("git", "--git-dir", str(self.bare), "for-each-ref", "--format=%(refname)", "refs/heads").stdout
        self.assertEqual(branches.strip(), "refs/heads/main")

    def test_candidate_flag_is_removed(self) -> None:
        text = (self.scripts / SCRIPT.name).read_text(encoding="utf-8")
        self.assertNotIn("--candidate", text)
        self.assertNotIn("--proved-tree", text)
        self.assertNotIn("--sync-mode", text)
        self.assertNotIn("--drift-analysis", text)
        self.assertNotIn("--drift-sha", text)
        self.assertNotIn("validates the rebased tree", text)
        self.assertNotIn("git rebase", text)

    def test_existing_pr_exact_idempotent_reuse(self) -> None:
        self.pr_state.write_text(
            json.dumps(
                [
                    {
                        "number": 9,
                        "url": "https://github.example/pull/9",
                        "baseRefName": "main",
                        "headRefOid": self.validated_commit,
                        "body": pr_body(
                            target_ref=TARGET_SHA,
                            target_kind="manual_commit",
                            target_sha=TARGET_SHA,
                            sync_commit=self.validated_commit,
                        ),
                    }
                ]
            )
            + "\n",
            encoding="utf-8",
        )
        result = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("pr_number=9\n", self.output.read_text(encoding="utf-8"))
        branches = run("git", "--git-dir", str(self.bare), "for-each-ref", "--format=%(refname)", "refs/heads").stdout
        self.assertEqual(branches.strip(), "refs/heads/main")

    def test_existing_pr_different_head_is_rejected(self) -> None:
        self.pr_state.write_text(
            json.dumps(
                [
                    {
                        "number": 4,
                        "url": "https://github.example/pull/4",
                        "baseRefName": "main",
                        "headRefOid": OTHER_SHA,
                        "body": pr_body(
                            target_ref=TARGET_SHA,
                            target_kind="manual_commit",
                            target_sha=TARGET_SHA,
                            sync_commit=OTHER_SHA,
                        ),
                    }
                ]
            )
            + "\n",
            encoding="utf-8",
        )
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not an exact publication of the validated commit", result.stderr)

    def test_existing_pr_wrong_base_is_rejected(self) -> None:
        self.pr_state.write_text(
            json.dumps(
                [
                    {
                        "number": 6,
                        "url": "https://github.example/pull/6",
                        "baseRefName": "release",
                        "headRefOid": self.validated_commit,
                        "body": pr_body(
                            target_ref=TARGET_SHA,
                            target_kind="manual_commit",
                            target_sha=TARGET_SHA,
                            sync_commit=self.validated_commit,
                            base_branch="release",
                        ),
                    }
                ]
            )
            + "\n",
            encoding="utf-8",
        )
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("base=", result.stderr)

    def test_existing_pr_target_kind_mismatch_is_rejected(self) -> None:
        self.pr_state.write_text(
            json.dumps(
                [
                    {
                        "number": 7,
                        "url": "https://github.example/pull/7",
                        "baseRefName": "main",
                        "headRefOid": self.validated_commit,
                        "body": pr_body(
                            target_ref=TARGET_SHA,
                            target_kind="stable_rust_tag",
                            target_sha=TARGET_SHA,
                            sync_commit=self.validated_commit,
                        ),
                    }
                ]
            )
            + "\n",
            encoding="utf-8",
        )
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("upstream_ref_kind=stable_rust_tag", result.stderr)

if __name__ == "__main__":
    unittest.main()

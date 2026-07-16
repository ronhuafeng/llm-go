#!/usr/bin/env python3

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("codexsdk_publish_sync_pr.sh")
TARGET_SHA = "a" * 40


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


class PublishSyncPrTest(unittest.TestCase):
    def test_publish_reuses_validation_and_refuses_divergent_branch_retry(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "repo"
            bare = Path(tmp) / "origin.git"
            scripts = root / "codexsdk/scripts"
            fake_bin = Path(tmp) / "bin"
            scripts.mkdir(parents=True)
            fake_bin.mkdir()
            shutil.copy2(SCRIPT, scripts / SCRIPT.name)
            (scripts / "codexsdk_validate_sync.sh").write_text(
                "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'validate\\n' >> \"${VALIDATION_LOG}\"\n",
                encoding="utf-8",
            )
            (scripts / "codexsdk_resolve_upstream.py").write_text(
                f"#!/usr/bin/env python3\nprint('{{\"peeled_commit_sha\": \"{TARGET_SHA}\"}}')\n",
                encoding="utf-8",
            )
            (fake_bin / "gh").write_text(
                textwrap.dedent(
                    """\
                    #!/usr/bin/env bash
                    set -euo pipefail
                    if [[ "$1 $2" == "pr list" ]]; then
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
                    echo "unexpected gh args: $*" >&2
                    exit 1
                    """
                ),
                encoding="utf-8",
            )
            for executable in (
                scripts / SCRIPT.name,
                scripts / "codexsdk_validate_sync.sh",
                scripts / "codexsdk_resolve_upstream.py",
                fake_bin / "gh",
            ):
                executable.chmod(0o755)

            run("git", "init", "-q", "--bare", str(bare))
            run("git", "init", "-q", "-b", "main", str(root))
            run("git", "config", "user.email", "codex@example.com", cwd=root)
            run("git", "config", "user.name", "Codex", cwd=root)
            run("git", "add", ".", cwd=root)
            run("git", "commit", "-q", "-m", "baseline", cwd=root)
            run("git", "remote", "add", "origin", str(bare), cwd=root)
            run("git", "push", "-q", "-u", "origin", "main", cwd=root)
            (root / "codexsdk/sync.txt").write_text("sync\n", encoding="utf-8")
            run("git", "add", "codexsdk/sync.txt", cwd=root)
            run("git", "commit", "-q", "-m", "sync", cwd=root)
            validated_commit = run("git", "rev-parse", "HEAD", cwd=root).stdout.strip()
            validation_log = Path(tmp) / "validation.log"
            output = Path(tmp) / "github-output.txt"
            env = {
                **os.environ,
                "GITHUB_OUTPUT": str(output),
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "VALIDATION_LOG": str(validation_log),
            }

            run(
                str(scripts / SCRIPT.name),
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
                validated_commit,
                "--sync-mode",
                "metadata-sync",
                cwd=root / "codexsdk",
                env=env,
            )

            self.assertEqual(validation_log.read_text(encoding="utf-8"), "validate\n")
            github_output = output.read_text(encoding="utf-8")
            self.assertIn(f"-{TARGET_SHA[:12]}\n", github_output)

            sync_branch = next(
                line.removeprefix("sync_branch=")
                for line in github_output.splitlines()
                if line.startswith("sync_branch=")
            )
            first_remote_commit = run(
                "git",
                "--git-dir",
                str(bare),
                "rev-parse",
                f"refs/heads/{sync_branch}",
            ).stdout.strip()
            (root / "codexsdk/sync.txt").write_text("different retry\n", encoding="utf-8")
            run("git", "add", "codexsdk/sync.txt", cwd=root)
            run("git", "commit", "-q", "-m", "different retry", cwd=root)
            retry_commit = run("git", "rev-parse", "HEAD", cwd=root).stdout.strip()

            retry = subprocess.run(
                [
                    str(scripts / SCRIPT.name),
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
                    retry_commit,
                    "--sync-mode",
                    "metadata-sync",
                ],
                cwd=root / "codexsdk",
                env=env,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            self.assertNotEqual(retry.returncode, 0)
            self.assertIn("Refusing to overwrite existing sync branch", retry.stderr)
            self.assertEqual(
                run("git", "--git-dir", str(bare), "rev-parse", f"refs/heads/{sync_branch}").stdout.strip(),
                first_remote_commit,
            )


if __name__ == "__main__":
    unittest.main()

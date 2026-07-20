#!/usr/bin/env python3

from __future__ import annotations

import os
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("codexsdk_dispose_superseded_sync_prs.sh")


def write_executable(path: Path, contents: str) -> None:
    path.write_text(textwrap.dedent(contents), encoding="utf-8")
    path.chmod(0o755)


class DisposeSupersededSyncPrsTest(unittest.TestCase):
    def run_cleanup(
        self,
        *,
        current_pr: str,
        pr_list: str,
        fail_branch_deletion: bool = False,
    ) -> tuple[str, str, str, str]:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fake_bin = root / "bin"
            fake_bin.mkdir()
            pr_state = root / "pr-state"
            branch_state = root / "branch-state"
            summary = root / "summary.md"
            pr_state.write_text("41=open\n42=open\n", encoding="utf-8")
            branch_state.write_text(
                "codex/sync-upstream/stale=present\n"
                "codex/sync-upstream/run-1-1-current=present\n"
                "codex/sync-upstream-legacy=present\n",
                encoding="utf-8",
            )
            write_executable(
                fake_bin / "gh",
                """\
                #!/usr/bin/env bash
                set -euo pipefail
                if [[ "$1 $2" == "auth setup-git" ]]; then
                  exit 0
                fi
                if [[ "$1 $2" == "pr list" ]]; then
                  printf '%s' "${FAKE_PR_LIST}"
                  exit 0
                fi
                if [[ "$1 $2" == "pr close" ]]; then
                  sed -i.bak "s/^$3=open$/$3=closed/" "${PR_STATE}"
                  rm -f "${PR_STATE}.bak"
                  exit 0
                fi
                exit 1
                """,
            )
            write_executable(
                fake_bin / "git",
                """\
                #!/usr/bin/env bash
                set -euo pipefail
                branch="${@: -1}"
                if [[ "${branch}" == "${FAIL_BRANCH}" ]]; then
                  exit 1
                fi
                sed -i.bak "s|^${branch}=present$|${branch}=deleted|" "${BRANCH_STATE}"
                rm -f "${BRANCH_STATE}.bak"
                """,
            )
            environment = {
                **os.environ,
                "BRANCH_STATE": str(branch_state),
                "FAIL_BRANCH": "codex/sync-upstream/stale" if fail_branch_deletion else "",
                "FAKE_PR_LIST": pr_list,
                "GITHUB_STEP_SUMMARY": str(summary),
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "PR_STATE": str(pr_state),
            }
            completed = subprocess.run(
                [
                    str(SCRIPT),
                    "--current-pr",
                    current_pr,
                    "--default-branch",
                    "main",
                    "--run-url",
                    "https://github.example/actions/runs/1",
                    "--upstream-ref",
                    "rust-v1.2.3",
                    "--upstream-sha",
                    "a" * 40,
                ],
                check=True,
                env=environment,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            return (
                completed.stdout,
                pr_state.read_text(encoding="utf-8"),
                branch_state.read_text(encoding="utf-8"),
                summary.read_text(encoding="utf-8"),
            )

    def test_preserves_replacement_and_warns_when_stale_branch_cannot_be_deleted(self) -> None:
        stdout, pr_state, branch_state, summary = self.run_cleanup(
            current_pr="42",
            pr_list="41\tcodex/sync-upstream/stale\n42\tcodex/sync-upstream/run-1-1-current\n",
            fail_branch_deletion=True,
        )

        self.assertIn("41=closed", pr_state)
        self.assertIn("42=open", pr_state)
        self.assertIn("codex/sync-upstream/stale=present", branch_state)
        self.assertIn("Superseded branch not deleted", stdout)
        self.assertIn("Disposed PR count: `1`", summary)
        self.assertIn("Preserved replacement PR: `#42`", summary)

    def test_closes_stale_prs_when_the_baseline_already_matches_target(self) -> None:
        _, pr_state, branch_state, summary = self.run_cleanup(
            current_pr="",
            pr_list="41\tcodex/sync-upstream-legacy\n",
        )

        self.assertIn("41=closed", pr_state)
        self.assertIn("codex/sync-upstream-legacy=deleted", branch_state)
        self.assertIn("Replacement PR: not required; baseline already matches the selected target", summary)


if __name__ == "__main__":
    unittest.main()

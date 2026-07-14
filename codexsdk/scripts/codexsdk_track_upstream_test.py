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


def write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def write_schema_baseline(root: Path) -> None:
    aggregate = {
        "oneOf": [
            {
                "properties": {
                    "method": {"enum": ["thread/start"]},
                    "params": {"$ref": "#/definitions/ThreadStartParams"},
                }
            }
        ]
    }
    write_json(root / "ClientRequest.json", aggregate)
    write_json(root / "ServerRequest.json", {"oneOf": []})
    write_json(root / "ServerNotification.json", {"oneOf": []})
    write_json(root / "ClientNotification.json", {"oneOf": []})
    write_json(root / "ThreadStartResponse.json", {"type": "object", "properties": {"value": {"type": "string"}}})


class TrackUpstreamTest(unittest.TestCase):
    def test_existing_binary_mode_entrypoint_writes_reports(self) -> None:
        repo = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            baseline = root / "baseline"
            codex_repo = root / "codex-repo"
            out = root / "out"
            write_schema_baseline(baseline)

            subprocess.run(["git", "init", "-q", str(codex_repo)], check=True)
            subprocess.run(["git", "-C", str(codex_repo), "config", "user.email", "codex@example.com"], check=True)
            subprocess.run(["git", "-C", str(codex_repo), "config", "user.name", "Codex"], check=True)
            subprocess.run(["git", "-C", str(codex_repo), "commit", "--allow-empty", "-q", "-m", "init"], check=True)
            commit = subprocess.check_output(["git", "-C", str(codex_repo), "rev-parse", "HEAD"], text=True).strip()

            fake_codex = root / "codex"
            fake_codex.write_text(
                "\n".join(
                    [
                        "#!/usr/bin/env python3",
                        "from pathlib import Path",
                        "import shutil",
                        "import sys",
                        f"source = Path({str(baseline)!r})",
                        "if sys.argv[1:] == ['--version']:",
                        "    print('codex-cli fake')",
                        "    raise SystemExit(0)",
                        "if sys.argv[1:4] == ['app-server', 'generate-json-schema', '--experimental'] and sys.argv[4] == '--out':",
                        "    shutil.copytree(source, Path(sys.argv[5]), dirs_exist_ok=True)",
                        "    raise SystemExit(0)",
                        "if sys.argv[1:3] == ['app-server', 'generate-json-schema'] and sys.argv[3] == '--out':",
                        "    shutil.copytree(source, Path(sys.argv[4]), dirs_exist_ok=True)",
                        "    raise SystemExit(0)",
                        "raise SystemExit(f'unexpected args: {sys.argv[1:]}')",
                        "",
                    ]
                ),
                encoding="utf-8",
            )
            fake_codex.chmod(0o755)

            completed = subprocess.run(
                [
                    str(repo / "scripts/codexsdk_track_upstream.sh"),
                    "--codex-repo",
                    str(codex_repo),
                    "--commit",
                    commit,
                    "--source-ref",
                    "rust-v0.141.0",
                    "--source-ref-kind",
                    "stable_rust_tag",
                    "--generator",
                    "binary",
                    "--codex-bin",
                    str(fake_codex),
                    "--baseline",
                    str(baseline),
                    "--out",
                    str(out),
                ],
                cwd=root,
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            reports = out / "reports"
            drift = json.loads((reports / "drift_summary.json").read_text(encoding="utf-8"))
            matrix = json.loads((reports / "matrix_update_skeleton.json").read_text(encoding="utf-8"))
            self.assertEqual(completed.stdout, "")
            self.assertEqual(completed.stderr, "")
            self.assertTrue((reports / "SUMMARY.md").exists())
            self.assertTrue((out / "schema" / "ClientRequest.json").exists())
            self.assertTrue((out / "stable-schema" / "ClientRequest.json").exists())
            self.assertEqual(drift["status"], "clean")
            self.assertEqual(sorted(drift.keys()), ["comparison_mode", "file_diff", "matrix_update_skeleton", "method_diff", "status", "target"])
            self.assertEqual(matrix["status"], "empty")

    def test_compare_only_json_prints_machine_result_only(self) -> None:
        repo = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            baseline = root / "baseline"
            out = root / "out"
            write_schema_baseline(baseline)

            completed = subprocess.run(
                [
                    str(repo / "scripts/codexsdk_track_upstream.sh"),
                    "--compare-only",
                    "--baseline",
                    str(baseline),
                    "--candidate",
                    str(baseline),
                    "--commit",
                    "1" * 40,
                    "--source-ref",
                    "rust-v0.141.0",
                    "--source-ref-kind",
                    "stable_rust_tag",
                    "--out",
                    str(out),
                    "--json",
                ],
                cwd=root,
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            payload = json.loads(completed.stdout)
            self.assertEqual(payload["status"], "clean")
            self.assertEqual(payload["source_commit"], "1" * 40)
            self.assertEqual(payload["schema"], str(baseline))
            self.assertEqual(payload["reports"], str(out / "reports"))
            self.assertEqual(completed.stderr, "")


if __name__ == "__main__":
    unittest.main()

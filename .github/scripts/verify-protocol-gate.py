#!/usr/bin/env python3
"""Launch the shared gate; preserve compiler/start failures as bounded JSONL."""
import json
import os
import pathlib
import selectors
import signal
import subprocess
import sys
import tempfile

LIMIT = 16 * 1024 * 1024

def main():
    if len(sys.argv) != 2 or sys.argv[1] not in {"source", "generated", "provenance", "live"}:
        return 2
    gate = sys.argv[1]
    root = pathlib.Path(os.environ["GITHUB_WORKSPACE"])
    directory = pathlib.Path(os.environ["RUNNER_TEMP"]) / ("goal-" + gate + "-diagnostics")
    directory.mkdir(parents=True, exist_ok=True)
    head = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
    command = ["go", "run", "./codexsdk/internal/cmd/protocolupgrade", "verify-ci-gate",
               "-gate", gate, "-repo-root", str(root), "-diagnostic-dir", str(directory)]
    chunks = {"stdout": bytearray(), "stderr": bytearray()}
    size, incomplete = 0, False
    try:
        process = subprocess.Popen(command, cwd=root, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
    except OSError:
        process = None
        status = 1
        chunks["stderr"].extend(b"gate process could not be started")
    if process is not None:
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ, "stdout")
        selector.register(process.stderr, selectors.EVENT_READ, "stderr")
        while selector.get_map():
            for key, _ in selector.select():
                data = os.read(key.fd, 65536)
                if not data:
                    selector.unregister(key.fileobj)
                    continue
                size += len(data)
                if size > LIMIT:
                    incomplete = True
                    try:
                        os.killpg(process.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                elif not incomplete:
                    chunks[key.data].extend(data)
        status = process.wait()
    path = pathlib.Path(chunks["stdout"].decode("utf-8", errors="replace").strip())
    # Only the fixed directory's regular JSONL is a gate result. Candidate stdout
    # cannot redirect artifact upload to a host credential file or another tree.
    if not incomplete and path.suffix == ".jsonl" and path.parent == directory and path.is_file() and not path.is_symlink():
        print(path)
        return status
    secrets = [v for k, v in os.environ.items() if len(v) >= 8 and
               (any(part in k for part in ("KEY", "TOKEN", "SECRET")) or k == "CODEX_RESPONSES_API_ENDPOINT")]
    def redact(text):
        for secret in sorted(secrets, key=len, reverse=True):
            text = text.replace(secret, "[redacted]")
        return text
    records = [{"kind": "ci_identity", "gate": gate, "head": head},
               {"kind": "gate_launch_failure", "gate": gate, "trust": "untrusted_command_output",
                "exit_code": status,
                "stdout": redact(chunks["stdout"].decode("utf-8", errors="replace")),
                "stderr": redact(chunks["stderr"].decode("utf-8", errors="replace"))},
               {"kind": "ci_result", "gate": gate, "head": head, "complete": not incomplete, "passed": False}]
    encoded = "".join(json.dumps(record, ensure_ascii=False) + "\n" for record in records).encode()
    if len(encoded) > LIMIT or incomplete:
        records[1] = {"kind": "diagnostics_incomplete", "gate": gate, "reason": "size_limit"}
        records[-1]["complete"] = False
        encoded = "".join(json.dumps(record) + "\n" for record in records).encode()
    descriptor, path = tempfile.mkstemp(prefix="launch-", suffix=".jsonl", dir=directory)
    with os.fdopen(descriptor, "wb") as output:
        output.write(encoded)
    print(path)
    return status or 1

if __name__ == "__main__":
    raise SystemExit(main())

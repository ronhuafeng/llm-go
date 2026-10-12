package repository

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoalGateBootstrapRetainsCompilerAndStartFailures(t *testing.T) {
	root, _ := filepath.Abs("../../../..")
	for _, scenario := range []string{"compile", "missing", "secret", "oversized", "unsafe_path"} {
		t.Run(scenario, func(t *testing.T) {
			work := t.TempDir()
			bin := filepath.Join(t.TempDir(), "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			git, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink(git, filepath.Join(bin, "git")); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@localhost", "commit", "-q", "--allow-empty", "-m", "base"}} {
				if b, e := exec.Command(git, append([]string{"-C", work}, args...)...).CombinedOutput(); e != nil {
					t.Fatalf("git %s %v", b, e)
				}
			}
			body := "#!/bin/sh\nprintf 'undefined: missing_symbol\\n' >&2\nexit 1\n"
			switch scenario {
			case "secret":
				body = "#!/bin/sh\nprintf 'prefix %s suffix\\n' \"$FIXTURE_SECRET\" >&2\nexit 1\n"
			case "oversized":
				body = "#!/usr/bin/python3\nimport sys\nsys.stderr.write('x'*(17*1024*1024))\nsys.exit(1)\n"
			case "unsafe_path":
				body = "#!/bin/sh\nprintf '/etc/passwd.jsonl\\n'\nexit 0\n"
			}
			if scenario != "missing" {
				if e := os.WriteFile(filepath.Join(bin, "go"), []byte(body), 0700); e != nil {
					t.Fatal(e)
				}
			}
			temp := t.TempDir()
			cmd := exec.Command("/usr/bin/python3", filepath.Join(root, ".github/scripts/verify-protocol-gate.py"), "source")
			cmd.Env = append(os.Environ(), "PATH="+bin, "GITHUB_WORKSPACE="+work, "RUNNER_TEMP="+temp, "FIXTURE_SECRET=private-marker-across-output")
			output, err := cmd.Output()
			if err == nil {
				t.Fatal("bootstrap failure reported success")
			}
			path := strings.TrimSpace(string(output))
			if filepath.Dir(path) != filepath.Join(temp, "goal-source-diagnostics") {
				t.Fatalf("invalid path %q", path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "private-marker-across-output") {
				t.Fatal("known secret leaked")
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) != 3 {
				t.Fatalf("missing complete records: %s", data)
			}
			var footer map[string]any
			if e := json.Unmarshal([]byte(lines[2]), &footer); e != nil {
				t.Fatal(e)
			}
			if footer["passed"] != false || footer["complete"] != (scenario != "oversized") {
				t.Fatalf("invalid footer %v", footer)
			}
			if scenario == "compile" && !strings.Contains(string(data), "undefined: missing_symbol") {
				t.Fatal("compiler root cause lost")
			}
		})
	}
}

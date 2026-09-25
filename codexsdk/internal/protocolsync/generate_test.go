package protocolsync

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateCandidateUsesLockedSourceAndRejectsBuildMutation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX cargo fixture")
	}
	for _, mutate := range []bool{false, true} {
		name := "unchanged"
		if mutate {
			name = "mutated source"
		}
		t.Run(name, func(t *testing.T) {
			upstream := t.TempDir()
			writeFile(t, filepath.Join(upstream, "codex-rs", "Cargo.lock"), "selected-lock\n")
			writeFile(t, filepath.Join(upstream, "codex-rs", "rust-toolchain.toml"), "selected-toolchain\n")
			writeFile(t, filepath.Join(upstream, "codex-rs", "app-server-protocol", "src", "protocol", "common.rs"), "exact common.rs\n")
			runGitInit(t, upstream)
			sha, err := gitOutput(upstream, "rev-parse", "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			sha = strings.TrimSpace(sha)
			module := t.TempDir()
			writeFile(t, filepath.Join(module, filepath.FromSlash(defaultBaselineRel), "Payload.json"), `{"type":"object"}`)
			bin := t.TempDir()
			calls := filepath.Join(t.TempDir(), "calls")
			script := `#!/bin/sh
set -eu
test "${RUSTUP_TOOLCHAIN+x}" != x
printf '%s\n' "$*" >> "$CARGO_CALLS"
test "$(cat rust-toolchain.toml)" = selected-toolchain
case " $* " in *" --locked "*) ;; *) echo 'lockfile updates are forbidden' >&2; exit 35;; esac
case " $* " in *" --version "*) echo 'codex-cli 1.2.3'; exit 0;; esac
if [ "$MUTATE_SOURCE" = true ]; then echo mutated > Cargo.lock; fi
while [ "$#" -gt 0 ]; do
 if [ "$1" = --out ]; then shift; mkdir -p "$1"; printf '%s' '{"type":"object"}' > "$1/Payload.json"; exit 0; fi
 shift
done
exit 36
`
			if err := os.WriteFile(filepath.Join(bin, "cargo"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("CARGO_CALLS", calls)
			t.Setenv("RUSTUP_TOOLCHAIN", "ambient-override")
			if mutate {
				t.Setenv("MUTATE_SOURCE", "true")
			} else {
				t.Setenv("MUTATE_SOURCE", "false")
			}
			candidate, err := GenerateCandidate(GenerateRequest{ModuleRoot: module, UpstreamRepo: upstream, Target: Target{RefName: "rust-v1.2.3", RefKind: KindStableTag, PeeledCommitSHA: sha}})
			if mutate {
				if err == nil || failureCategory(err) != FailureSource || candidate.Dir != "" {
					t.Fatalf("mutated build accepted: candidate=%+v err=%v", candidate, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if candidate.SourceCommit != sha || candidate.DriftStatus != "clean" {
				t.Fatalf("candidate=%+v", candidate)
			}
			raw, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) != 3 {
				t.Fatalf("cargo calls = %s", raw)
			}
			for _, line := range lines {
				if !strings.HasPrefix(line, "run --locked -p codex-cli -- ") {
					t.Fatalf("unlocked cargo invocation %q", line)
				}
			}
			common, err := os.ReadFile(candidate.CommonRS)
			if err != nil || string(common) != "exact common.rs\n" {
				t.Fatalf("common.rs = %q err=%v", common, err)
			}
		})
	}
}

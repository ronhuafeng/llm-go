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
	for _, name := range []string{"unchanged", "mutated source", "cached cargo config", "parent cargo config", "cached rustup override"} {
		mutate := name == "mutated source"
		config := strings.Contains(name, "cargo config") || name == "cached rustup override"
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
test "${RUSTFLAGS+x}" != x
test "${CARGO_ENCODED_RUSTFLAGS+x}" != x
test "${RUSTC_WRAPPER+x}" != x
printf '%s\n' "$*" >> "$FIXTURE_CALLS"
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
			rustup := "#!/bin/sh\nset -eu\ntest \"$*\" = 'override list'\nprintf '%s\\n' \"$FIXTURE_OVERRIDES\"\n"
			if err := os.WriteFile(filepath.Join(bin, "rustup"), []byte(rustup), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FIXTURE_OVERRIDES", "no overrides")
			if name == "cached rustup override" {
				t.Setenv("FIXTURE_OVERRIDES", module+"\tambient-toolchain")
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FIXTURE_CALLS", calls)
			t.Setenv("RUSTUP_TOOLCHAIN", "ambient-override")
			t.Setenv("RUSTFLAGS", "--cfg ambient")
			t.Setenv("CARGO_ENCODED_RUSTFLAGS", "--cfg=ambient")
			t.Setenv("RUSTC_WRAPPER", "/ambient/wrapper")
			if name == "cached cargo config" {
				writeFile(t, filepath.Join(module, ".cache", "cargo-home", "config.toml"), "[build]\nrustflags = ['--cfg=ambient']\n")
			}
			if name == "parent cargo config" {
				writeFile(t, filepath.Join(module, ".cargo", "config"), "[build]\nrustflags = ['--cfg=ambient']\n")
			}
			if mutate {
				t.Setenv("MUTATE_SOURCE", "true")
			} else {
				t.Setenv("MUTATE_SOURCE", "false")
			}
			candidate, err := GenerateCandidate(GenerateRequest{ModuleRoot: module, UpstreamRepo: upstream, Target: Target{RefName: "rust-v1.2.3", RefKind: KindStableTag, PeeledCommitSHA: sha}})
			if mutate || config {
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

package repository

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDraftReleasePlatformSmoke(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repoctl := filepath.Join(t.TempDir(), "repoctl")
	build := exec.Command("go", "build", "-o", repoctl, "./cmd/repoctl")
	build.Dir = filepath.Join(root, "internal", "tools")
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build repoctl: %v: %s", err, output)
	}

	tests := []struct {
		name             string
		visibleAfter     int
		maxAttempts      int
		preexisting      bool
		wrongTarget      bool
		wantExit         int
		wantCreates      int
		wantAPICalls     int
		wantObservations int
	}{
		{name: "eventually visible", visibleAfter: 3, maxAttempts: 4, wantCreates: 1, wantAPICalls: 4, wantObservations: 3},
		{name: "visibility exhausted", visibleAfter: 99, maxAttempts: 3, wantExit: 3, wantCreates: 1, wantAPICalls: 4, wantObservations: 3},
		{name: "malformed observation", visibleAfter: 1, maxAttempts: 4, wrongTarget: true, wantExit: 1, wantCreates: 1, wantAPICalls: 2, wantObservations: 1},
		{name: "reuse exact preexisting Draft", preexisting: true, maxAttempts: 4, wantAPICalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := t.TempDir()
			bin := t.TempDir()
			writeFakeGH(t, filepath.Join(bin, "gh"))
			notes := filepath.Join(t.TempDir(), "notes.md")
			if err := os.WriteFile(notes, []byte("notes\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("bash", filepath.Join(root, ".github", "scripts", "ensure-draft-release.sh"))
			command.Dir = root
			command.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"REPOCTL="+repoctl,
				"TAG=codexsdk/v0.6.0",
				"COMMIT="+strings.Repeat("a", 40),
				"NOTES="+notes,
				"GITHUB_REPOSITORY=ronhuafeng/llm-go",
				"RUNNER_TEMP="+state,
				"FAKE_GH_STATE="+state,
				fmt.Sprintf("FAKE_VISIBLE_AFTER=%d", test.visibleAfter),
				fmt.Sprintf("FAKE_PREEXISTING=%t", test.preexisting),
				fmt.Sprintf("FAKE_WRONG_TARGET=%t", test.wrongTarget),
				fmt.Sprintf("DRAFT_VISIBILITY_ATTEMPTS=%d", test.maxAttempts),
				"DRAFT_VISIBILITY_DELAY_SECONDS=0",
			)
			output, err := command.CombinedOutput()
			gotExit := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("run Draft Release smoke: %v: %s", err, output)
				}
				gotExit = exitErr.ExitCode()
			}
			if gotExit != test.wantExit {
				t.Fatalf("exit = %d, want %d: %s", gotExit, test.wantExit, output)
			}
			assertCounter(t, state, "create-count", test.wantCreates)
			assertCounter(t, state, "api-count", test.wantAPICalls)
			assertCounter(t, state, "observation-count", test.wantObservations)
		})
	}
}

func writeFakeGH(t *testing.T, path string) {
	t.Helper()
	const source = `#!/usr/bin/env bash
set -euo pipefail

increment() {
  local name=$1
  local value=0
  if [ -f "$FAKE_GH_STATE/$name" ]; then
    value=$(cat "$FAKE_GH_STATE/$name")
  fi
  value=$((value + 1))
  printf '%s' "$value" > "$FAKE_GH_STATE/$name"
  printf '%s' "$value"
}

release_json() {
  local target=$COMMIT
  if [ "$FAKE_WRONG_TARGET" = true ]; then
    target=main
  fi
  printf '[[{"id":42,"tag_name":"%s","target_commitish":"%s","draft":true,"prerelease":false}]]\n' "$TAG" "$target"
}

case "$1" in
  api)
    test "$#" -eq 4
    test "$2" = --paginate
    test "$3" = --slurp
    test "$4" = "repos/$GITHUB_REPOSITORY/releases?per_page=100"
    increment api-count >/dev/null
    if [ "$FAKE_PREEXISTING" = true ]; then
      release_json
    elif [ -f "$FAKE_GH_STATE/created" ]; then
      observation=$(increment observation-count)
      if [ "$observation" -ge "$FAKE_VISIBLE_AFTER" ]; then
        release_json
      else
        printf '[]\n'
      fi
    else
      printf '[]\n'
    fi
    ;;
  release)
    expected=(release create "$TAG" --repo "$GITHUB_REPOSITORY" --verify-tag --target "$COMMIT" --draft --title "$TAG (verification pending)" --notes-file "$NOTES")
    test "$#" -eq "${#expected[@]}"
    index=0
    for argument in "$@"; do
      test "$argument" = "${expected[$index]}"
      index=$((index + 1))
    done
    increment create-count >/dev/null
    if [ -f "$FAKE_GH_STATE/created" ]; then
      echo "duplicate Draft creation" >&2
      exit 91
    fi
    : > "$FAKE_GH_STATE/created"
    echo "https://example.invalid/release/42"
    ;;
  *)
    echo "unexpected gh command: $*" >&2
    exit 92
    ;;
esac
`
	if err := os.WriteFile(path, []byte(source), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertCounter(t *testing.T, directory, name string, want int) {
	t.Helper()
	path := filepath.Join(directory, name)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && want == 0 {
		return
	}
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	got, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	if got != want {
		t.Errorf("%s = %d, want %d", name, got, want)
	}
}

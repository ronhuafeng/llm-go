package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunNormalizesMiniCredentialBeforeLaunchingTests(t *testing.T) {
	const key = "private-runner-fixture-key"
	root := t.TempDir()
	metadata := filepath.Join(root, "codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json")
	if err := os.MkdirAll(filepath.Dir(metadata), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata, []byte(`{"codex_version":"codex-cli 0.159.0","source_ref_name":"rust-v0.159.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{
		"git": "#!/bin/sh\nprintf '%040d\\n' 1\n",
		"go": `#!/bin/sh
[ "$MINI_CODEX_API_KEY" = 'private-runner-fixture-key' ] || exit 42
[ -z "$GH_TOKEN$GITHUB_TOKEN$AUTO_FORWARD_APP_PRIVATE_KEY$PROTOCOL_SYNC_APP_PRIVATE_KEY" ] || exit 43
[ "$LLMGO_LIVE_CODEX" = 1 ] || exit 44
: > child-started
printf '%s\n' '{"Action":"run","Test":"TestLiveCodexFixture"}' '{"Action":"pass","Test":"TestLiveCodexFixture"}'
`,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "AUTO_FORWARD_APP_PRIVATE_KEY", "PROTOCOL_SYNC_APP_PRIVATE_KEY"} {
		t.Setenv(name, "write-authority-fixture")
	}
	for _, test := range []struct {
		name, input string
		failure     bool
	}{
		{name: "plain", input: key},
		{name: "surrounding LF", input: "\n" + key + "\n"},
		{name: "surrounding CRLF and space", input: " \t\r\n" + key + "\r\n \t"},
		{name: "embedded newline", input: key + "\nsuffix", failure: true},
		{name: "embedded space", input: key + " suffix", failure: true},
		{name: "blank", input: " \t\r\n", failure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("MINI_CODEX_API_KEY", test.input)
			err := run([]string{"run"})
			if (err != nil) != test.failure {
				t.Fatalf("failure=%v, err=%v", test.failure, err)
			}
			if err != nil && strings.Contains(err.Error(), key) {
				t.Fatal("credential appeared in diagnostic")
			}
			_, statErr := os.Stat("child-started")
			if test.failure {
				if !os.IsNotExist(statErr) {
					t.Fatal("malformed credential launched tests")
				}
			} else if statErr != nil {
				t.Fatal("canonical credential did not reach the child test process")
			}
			_ = os.Remove("child-started")
		})
	}
}

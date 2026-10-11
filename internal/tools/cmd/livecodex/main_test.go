package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunNormalizesCanonicalCredentialBeforeLaunchingTests(t *testing.T) {
	const key = "private-runner-fixture-key"
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{
		"git": "#!/bin/sh\nprintf '%040d\\n' 1\n",
		"go": `#!/bin/sh
[ "$AZURE_OPENAI_API_KEY" = 'private-runner-fixture-key' ] || exit 42
[ "$CODEX_RESPONSES_API_ENDPOINT" = 'https://provider.example/v1/responses' ] || exit 45
[ -z "$GH_TOKEN$GITHUB_TOKEN$AUTO_FORWARD_APP_PRIVATE_KEY$PROTOCOL_SYNC_APP_PRIVATE_KEY" ] || exit 43
[ "$LLMGO_LIVE_CODEX" = 1 ] || exit 44
[ "$CODEX_CLI_VERSION" = 0.171.2 ] || exit 46
[ "$LLMGO_LIVE_CODEX_MODEL" = gpt-6-sol ] || exit 47
[ "$LLMGO_LIVE_CODEX_REASONING" = high ] || exit 48
: > child-started
printf '%s\n' '{"Action":"run","Test":"TestLiveCodexFixture"}' '{"Action":"pass","Test":"TestLiveCodexFixture"}'
`,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	t.Setenv("CODEX_CLI_VERSION", "0.171.2")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEX_RESPONSES_API_ENDPOINT", "https://provider.example/v1/responses")
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
			t.Setenv("AZURE_OPENAI_API_KEY", test.input)
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

func TestRunRequiresSelectedRuntime(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_KEY", "fixture-key")
	for _, version := range []string{"", "latest", "0.171.2-alpha.1"} {
		t.Setenv("CODEX_CLI_VERSION", version)
		err := run([]string{"run"})
		if err == nil || !strings.Contains(err.Error(), "CODEX_CLI_VERSION") {
			t.Fatalf("unselected runtime: %v", err)
		}
	}
}

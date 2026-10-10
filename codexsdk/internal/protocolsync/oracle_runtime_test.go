package protocolsync

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestAcceptedBaselineEvidenceRequiresEveryTest(t *testing.T) {
	const pkg = "github.com/ronhuafeng/llm-go/codexsdk"
	for _, tc := range []struct {
		name    string
		actions []string
		want    bool
	}{
		{"all run and pass", []string{"run:A", "pass:A", "run:B", "pass:B"}, true},
		{"exit zero", nil, false},
		{"partial", []string{"run:A", "pass:A"}, false},
		{"skipped", []string{"run:A", "pass:A", "run:B", "skip:B"}, false},
		{"pass without execution", []string{"pass:A", "pass:B"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := baselineTestEvents{expected: map[string]bool{pkg + "/TestA": true, pkg + "/TestB": true}, ran: map[string]bool{}, passed: map[string]bool{}}
			for _, action := range tc.actions {
				n := len(action)
				b, _ := json.Marshal(map[string]string{"Action": action[:n-2], "Test": "Test" + action[n-1:], "Package": pkg})
				w.Write(append(b, '\n'))
			}
			if got := w.verify() == nil; got != tc.want {
				t.Fatalf("proof=%t want=%t: %v", got, tc.want, w.verify())
			}
		})
	}
}
func TestAcceptedBaselineSelectionUsesTrustedTestFiles(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{".", "protocolv2", "internal/wirejson"} {
		writeFile(t, filepath.Join(root, rel, "fixture.go"), "package fixture\n")
	}
	writeFile(t, filepath.Join(root, "fixture_test.go"), "package fixture\nimport \"testing\"\nfunc TestAccepted(t *testing.T){}\nfunc TestMain(m *testing.M){}\nfunc Testhelper(){}\nfunc TestFuzzlower(t *testing.T){}\nfunc FuzzAccepted(f *testing.F){}\nfunc Example(){ // Output: ok\n}\n")
	got, err := acceptedBaselineTests(root)
	if err != nil || len(got) != 4 || !got["github.com/ronhuafeng/llm-go/codexsdk/TestAccepted"] || !got["github.com/ronhuafeng/llm-go/codexsdk/FuzzAccepted"] || !got["github.com/ronhuafeng/llm-go/codexsdk/Example"] {
		t.Fatalf("selection %v %v", got, err)
	}
}

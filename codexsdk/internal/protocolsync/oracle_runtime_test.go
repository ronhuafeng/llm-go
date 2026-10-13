package protocolsync

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOracleGoalEvidenceSurvivesRealPassingProbeVolume(t *testing.T) {
	const pkg = "github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe"
	const secret = "oracle-volume-secret-472916"
	t.Setenv("ORACLE_LOG_TEST_SECRET", secret)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module "+pkg+"\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "contract_test.go"), `package fixture
import("testing";"fmt";"os")
func TestTrustedSchemaOracle(t *testing.T){
 for i:=0;i<20000;i++{t.Run(fmt.Sprintf("ClientRequest.json/probe_%d",i),func(t *testing.T){})}
 if os.Getenv("ORACLE_LOG_TEST_FAIL")=="1"{t.Run("ClientRequest.json/probe_20000",func(t *testing.T){t.Fatalf("LATE_CONTRACT_FAILURE token=%s",os.Getenv("ORACLE_LOG_TEST_SECRET"))})}
}
`)
	for _, tc := range []struct {
		name, flag string
		pass       bool
	}{{"pass", "0", true}, {"late failure", "1", false}} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("go", "test", "-count=1", "-json", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off", "ORACLE_LOG_TEST_FAIL="+tc.flag)
			out, runErr := cmd.Output()
			if (runErr == nil) != tc.pass || len(out) < goalDiagnosticLimit {
				t.Fatalf("fixture process=%v output bytes=%d", runErr, len(out))
			}
			d, err := newGoalDiagnostics(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer d.file.Close()
			output := &goalOutput{log: d, gate: "provenance"}
			events := oracleTestEvents{output: output, failure: func(test string) error {
				if strings.HasSuffix(test, "/probe_20000") {
					_, err := io.WriteString(output, "{\"kind\":\"schema_probe_expectation\",\"trust\":\"source-derived\",\"marker\":\"LATE_EXPECTATION\"}\n")
					return err
				}
				return nil
			}}
			for len(out) > 0 {
				n := min(3337, len(out))
				if _, err := events.Write(out[:n]); err != nil {
					t.Fatal(err)
				}
				out = out[n:]
			}
			if events.invalid || events.pending != "" || !events.started || events.passed != tc.pass || events.failed == tc.pass {
				t.Fatal("full process stream lost its original verdict")
			}
			output.flush(true)
			d.record("footer", "provenance", map[string]any{"complete": true, "passed": tc.pass})
			if d.err != nil {
				t.Fatalf("passing status volume displaced terminal evidence: %v", d.err)
			}
			data, err := os.ReadFile(d.file.Name())
			if err != nil {
				t.Fatal(err)
			}
			if len(data) > 256<<10 || bytes.Contains(data, []byte(secret)) {
				t.Fatal("diagnostics retained status noise or a configured secret")
			}
			var text strings.Builder
			footer := false
			decoder := json.NewDecoder(bytes.NewReader(data))
			for {
				var record map[string]any
				if err := decoder.Decode(&record); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if value, ok := record["output"].(string); ok {
					text.WriteString(value)
				}
				if record["kind"] == "footer" {
					footer = record["complete"] == true && record["passed"] == tc.pass
				}
			}
			if !footer || (!tc.pass && (!strings.Contains(text.String(), "LATE_CONTRACT_FAILURE") || !strings.Contains(text.String(), "LATE_EXPECTATION") || !strings.Contains(text.String(), "[redacted]"))) {
				t.Fatal("terminal result or late failure evidence was lost")
			}
		})
	}
}

func TestOracleRetainsRealGoBuildDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.invalid/oraclebuild\n\ngo 1.26\n")
	writeFile(t, filepath.Join(root, "contract_test.go"), "package oraclebuild\nimport \"testing\"\nfunc TestTrustedSchemaOracle(t *testing.T) { _ = missingContractBinding }\n")
	cmd := exec.Command("go", "test", "-json", ".")
	cmd.Dir = root
	out, err := cmd.Output()
	if err == nil {
		t.Fatal("broken trusted fixture unexpectedly compiled")
	}
	var target oracleTestEvents
	baseline := baselineTestEvents{expected: map[string]bool{"example.invalid/oraclebuild/TestTrustedSchemaOracle": true}, ran: map[string]bool{}, passed: map[string]bool{}}
	// Preserve framing when BuildEvents arrive across arbitrary writes.
	for _, b := range out {
		_, _ = target.Write([]byte{b})
		_, _ = baseline.Write([]byte{b})
	}
	for name, diagnostic := range map[string]string{"target": target.diagnostic.String(), "baseline": baseline.diagnostic.String()} {
		if !strings.Contains(diagnostic, "contract_test.go:") || !strings.Contains(diagnostic, "undefined: missingContractBinding") {
			t.Fatalf("%s lost actual compiler diagnostic: %q; raw=%s", name, diagnostic, out)
		}
	}
	if target.started || target.passed || baseline.verify() == nil {
		t.Fatal("build failure was accepted as executed test evidence")
	}
}

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

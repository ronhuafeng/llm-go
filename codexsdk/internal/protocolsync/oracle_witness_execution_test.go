package protocolsync

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

func TestGeneratedOracleWitnessesAndFailureEvidence(t *testing.T) {
	const pkg = "github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe"
	r := schemaoracle.Request{
		Schemas:      map[string][]byte{"Item.json": []byte(`{"type":"object","properties":{"item":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}},"required":["item"]}`)},
		Bindings:     map[string]string{"Item.json": "Payload"},
		Package:      "schemaoracleprobe",
		CloseObjects: true,
	}
	source, err := schemaoracle.Generate(r)
	if err != nil {
		t.Fatal(err)
	}
	const decoder = `package schemaoracleprobe
import("encoding/json";"fmt")
type Payload struct{Item json.RawMessage ` + "`json:\"item\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var root map[string]json.RawMessage;if json.Unmarshal(b,&root)!=nil||len(root)!=1||root["item"]==nil{return fmt.Errorf("item required")};item:=root["item"];CHECK;var object map[string]any;if json.Unmarshal(item,&object)!=nil||len(object)!=1{return fmt.Errorf("item object")};if _,ok:=object["id"].(string);!ok{return fmt.Errorf("id")};p.Item=item;return nil}
`
	good := strings.Replace(decoder, "CHECK", "", 1)
	bad := strings.Replace(decoder, "CHECK", `if string(item)=="null"||string(item)=="false"||string(item)=="{}"{p.Item=item;return nil}`, 1)
	for _, tc := range []struct {
		name, code, cause string
		noTest, pass      bool
	}{
		{"correct_decoder", good, "", false, true},
		{"three_invalid_witnesses", bad, "schema-invalid input accepted:", false, false},
		{"compiler", good + "\nvar _ = missingOracleBinding\n", "undefined: missingOracleBinding", false, false},
		{"panic", strings.Replace(good, "var root", `panic("ORACLE_WITNESS_PANIC");var root`, 1), "ORACLE_WITNESS_PANIC", false, false},
		{"missing_trusted_test", good, "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "go.mod"), "module "+pkg+"\n\ngo 1.26.8\n")
			writeFile(t, filepath.Join(dir, "payload.go"), tc.code)
			writeFile(t, filepath.Join(dir, "contract_test.go"), string(source))
			args := []string{"test", "-count=1", "-json"}
			if tc.noTest {
				args = append(args, "-run", "^MissingTrustedTest$")
			}
			cmd := exec.Command("go", append(args, ".")...)
			cmd.Dir = dir
			out, runErr := cmd.Output()
			var events oracleTestEvents
			for _, b := range out {
				_, _ = events.Write([]byte{b})
			}
			// The same predicates own runTargetOracle success; a build, panic,
			// or zero-exit missing test cannot supply executed proof.
			passed := runErr == nil && events.started && events.passed && !events.failed && !events.invalid && events.pending == ""
			if passed != tc.pass {
				t.Fatalf("proof=%t want=%t process=%v events=%+v", passed, tc.pass, runErr, events)
			}
			diagnostic := boundedRepairDiagnostic(events.diagnostic.String())
			if tc.cause != "" && !strings.Contains(diagnostic, tc.cause) {
				t.Fatalf("missing actual failure cause %q: %s", tc.cause, diagnostic)
			}
			if len(diagnostic) > 8192 || len(events.diagnostic.String()) > 6144 {
				t.Fatal("diagnostic bounds increased")
			}
			if tc.name == "three_invalid_witnesses" {
				t.Logf("bounded native feedback from one execution:\n%s", diagnostic)
				for _, value := range []string{"null", "false", "{}"} {
					if !strings.Contains(diagnostic, `"item":`+value) {
						t.Errorf("one-pass native feedback omitted %s: %s", value, diagnostic)
					}
				}
				failedProbes := 0
				for _, line := range strings.Split(string(out), "\n") {
					var event struct{ Action, Test string }
					if json.Unmarshal([]byte(line), &event) == nil && event.Action == "fail" && strings.Contains(event.Test, "/probe_") {
						failedProbes++
					}
				}
				if failedProbes != 3 {
					t.Fatalf("actual failed probe events=%d want=3: %s", failedProbes, diagnostic)
				}
			}
			if tc.noTest && (runErr != nil || events.started || events.passed) {
				t.Fatal("missing-test counterexample did not exercise zero-exit without proof")
			}
		})
	}
}

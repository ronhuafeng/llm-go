package protocolsync

import (
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
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
					if !strings.Contains(diagnostic, "value="+map[string]string{"null": "null", "false": "false", "{}": "<object members=0>"}[value]) {
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

// Compile and run the frozen oracle, then feed native go-test events through the
// 4 KiB failure and 8 KiB summary bounds. A wide request formerly hid
// every late field behind repeated payload prefixes.
func TestCompactOracleWideEnvelopeFeedback(t *testing.T) {
	const secret = "private-oracle-url-and-body-28473"
	t.Setenv("ORACLE_BODY_SECRET", secret)
	padding := strings.Repeat("wide-body-", 260) + "https://user:" + secret + "@example.test/?token=" + secret
	props := map[string]any{"aaaPadding": map[string]any{"const": padding}}
	required := []string{"aaaPadding"}
	for _, field := range []string{"effortA", "effortB", "effortC", "effortD", "effortE"} {
		props[field] = map[string]any{"$ref": "#/definitions/ReasoningEffort"}
		required = append(required, field)
	}
	schema, _ := json.Marshal(map[string]any{"definitions": map[string]any{"ReasoningEffort": map[string]any{"type": "string", "minLength": 1}}, "anyOf": []any{map[string]any{"type": "null"}, map[string]any{"type": "integer"}, map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}}})
	request := schemaoracle.Request{Schemas: map[string][]byte{"Envelope.json": schema}, Bindings: map[string]string{"Envelope.json": "Payload"}, Package: "schemaoracleprobe", CloseObjects: true}
	targets, err := schemaoracle.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	accepted := map[string]bool{}
	invalidCount := 0
	for _, p := range targets[0].Probes {
		if p.Valid {
			accepted[p.JSON] = true
		}
		if !p.Valid && strings.Contains(p.Diagnostic, "ReasoningEffort/minLength") {
			accepted[p.JSON] = true
			invalidCount++
		}
	}
	if invalidCount != 5 {
		t.Fatalf("fixture invalid probes=%d", invalidCount)
	}
	acceptedJSON, _ := json.Marshal(accepted)
	decoder := `package schemaoracleprobe
import("encoding/json";"fmt")
type Payload struct{raw json.RawMessage}
var accepted map[string]bool
func init(){json.Unmarshal([]byte(` + strconv.Quote(string(acceptedJSON)) + `),&accepted)}
func(p *Payload)UnmarshalJSON(b []byte)error{if !accepted[string(b)]{return fmt.Errorf("invalid")};p.raw=append(p.raw[:0],b...);return nil}
func(p Payload)MarshalJSON()([]byte,error){return p.raw,nil}
`
	generated, err := schemaoracle.Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, compact := range []bool{false, true} {
		source := string(generated)
		if !compact {
			source = strings.Replace(source, "i, frozen.Diagnostics[p.Diagnostic])", "i, wire[:min(len(wire),2048)])", 1)
			if source == string(generated) {
				t.Fatal("legacy fixture replacement failed")
			}
		}
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe\n\ngo 1.26.8\n")
		writeFile(t, filepath.Join(dir, "payload.go"), decoder)
		writeFile(t, filepath.Join(dir, "contract_test.go"), source)
		cmd := exec.Command("go", "test", "-count=1", "-json", ".")
		cmd.Dir = dir
		out, runErr := cmd.Output()

		var expectations strings.Builder
		var events oracleTestEvents
		events.failure = func(test string) error { return writeTargetOracleProbe(&expectations, targets, test) }
		events.Write(out)
		count := 0
		for _, line := range strings.Split(strings.TrimSpace(expectations.String()), "\n") {
			var record struct {
				Kind, Trust, Schema string
				Index               int
				Input               map[string]any
				Location            struct{ Source string }
			}
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatal(err)
			}
			if record.Kind != "schema_probe_expectation" || record.Trust != "source-derived" || record.Input["aaaPadding"] != padding || !strings.Contains(record.Location.Source, "ReasoningEffort/minLength") {
				t.Fatal("full source witness or location lost")
			}
			count++
		}
		if count != 5 {
			t.Fatalf("source expectations=%d", count)
		}

		if runErr == nil || !events.started || !events.failed || events.passed || events.invalid {
			t.Fatalf("false acceptance compact=%t: %v %+v", compact, runErr, events)
		}
		diagnostic := boundedRepairDiagnostic(events.diagnostic.String())
		if len(diagnostic) > 8192 || len(events.diagnostic.String()) > 6144 {
			t.Fatal("expanded feedback budget")
		}
		if strings.Contains(diagnostic, secret) {
			t.Fatal("secret escaped existing redactor")
		}
		for _, field := range required[1:] {
			present := strings.Contains(diagnostic, "instance=#/"+field)
			if present != compact {
				t.Fatalf("compact=%t path=%s retained=%s", compact, field, diagnostic)
			}
		}
		if compact && (!strings.Contains(diagnostic, "Envelope.json#/definitions/ReasoningEffort/minLength") || !strings.Contains(diagnostic, `minLength: got 0, want 1 value=""`)) {
			t.Fatal(diagnostic)
		}
		t.Logf("compact=%t feedback bytes=%d", compact, len(diagnostic))
	}
}

func TestRoundtripFeedbackPreservesLateObservedDifference(t *testing.T) {
	padding := strings.Repeat("padding-", 400)
	raw, _ := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{"aaa": map[string]any{"const": padding}, "zzz": map[string]any{"type": "string"}}, "required": []string{"aaa", "zzz"}})
	req := schemaoracle.Request{Schemas: map[string][]byte{"Loss.json": raw}, Bindings: map[string]string{"Loss.json": "Payload"}, Package: "schemaoracleprobe", CloseObjects: true}
	targets, e := schemaoracle.Build(req)
	if e != nil {
		t.Fatal(e)
	}
	source, e := schemaoracle.Render(req, targets)
	if e != nil {
		t.Fatal(e)
	}
	accepted := map[string]bool{}
	for _, p := range targets[0].Probes {
		if p.Valid {
			accepted[p.JSON] = true
		}
	}
	encoded, _ := json.Marshal(accepted)
	code := `package schemaoracleprobe
import("encoding/json";"fmt")
type Payload struct{AAA string ` + "`json:\"aaa\"`" + `;ZZZ string ` + "`json:\"zzz\"`" + `}
var accepted map[string]bool
func init(){json.Unmarshal([]byte(` + strconv.Quote(string(encoded)) + `),&accepted)}
func(p *Payload)UnmarshalJSON(b []byte)error{if !accepted[string(b)]{return fmt.Errorf("invalid")};var v map[string]string;json.Unmarshal(b,&v);p.AAA=v["aaa"];p.ZZZ="LATE_OBSERVED_DIFFERENCE";return nil}
`
	work := t.TempDir()
	writeFile(t, filepath.Join(work, "go.mod"), "module github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe\n\ngo 1.26.8\n")
	writeFile(t, filepath.Join(work, "payload.go"), code)
	writeFile(t, filepath.Join(work, "contract_test.go"), string(source))
	var events oracleTestEvents
	var rawEvents strings.Builder
	cmd := exec.Command("go", "test", "-count=1", "-json", ".")
	cmd.Dir = work
	cmd.Stdout = io.MultiWriter(&events, &rawEvents)
	e = cmd.Run()
	if e == nil || !events.failed {
		t.Fatal("lossy decoder accepted")
	}
	var output strings.Builder
	for _, line := range strings.Split(rawEvents.String(), "\n") {
		var event struct{ Output string }
		if json.Unmarshal([]byte(line), &event) == nil {
			output.WriteString(event.Output)
		}
	}
	if !strings.Contains(output.String(), padding) || !strings.Contains(output.String(), "LATE_OBSERVED_DIFFERENCE") {
		t.Fatal("late actual value truncated before stream")
	}
}

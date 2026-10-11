package protocolsync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const acceptanceBaselineSchema = `{"type":"object","properties":{"base":{"type":"boolean"}},"required":["base"]}`
const acceptanceTargetSchema = `{"type":"object","properties":{"base":{"type":"boolean"},"added":{"type":"string"}},"required":["base"]}`
const acceptanceMovingTest = `package schemaoracle
import("os";"testing";"encoding/json";"reflect")
func TestContract(t *testing.T){b,e:=os.ReadFile("../protocolschema/appserver/v2/Payload.json");if e!=nil{t.Fatal(e)};var schema struct{Properties map[string]json.RawMessage};if e=json.Unmarshal(b,&schema);e!=nil{t.Fatal(e)};var baselineFixture struct{Base bool};if len(schema.Properties)!=reflect.TypeOf(baselineFixture).NumField(){t.Fatal("schema and frozen fixture versions differ")}}
`
const acceptanceFixedTest = `package schemaoracle
import("encoding/json";"testing";"reflect")
func TestContract(t *testing.T){const frozenSchema=` + "`" + acceptanceBaselineSchema + "`" + `;var schema struct{Properties map[string]json.RawMessage};if e:=json.Unmarshal([]byte(frozenSchema),&schema);e!=nil{t.Fatal(e)};var baselineFixture struct{Base bool};if len(schema.Properties)!=reflect.TypeOf(baselineFixture).NumField(){t.Fatal("frozen source and fixture disagree")}}
`

func acceptanceFixture(t *testing.T, oldTest, newTest string) (string, string, string) {
	t.Helper()
	old := filepath.Join(t.TempDir(), "old")
	writeFile(t, filepath.Join(old, "go.mod"), "module github.com/ronhuafeng/llm-go\n\ngo 1.26.8\n")
	writeFile(t, filepath.Join(old, mechanicalPrefix, "Payload.json"), acceptanceBaselineSchema)
	writeFile(t, filepath.Join(old, "codexsdk/internal/schemaoracle/oracle.go"), "package schemaoracle\n")
	writeFile(t, filepath.Join(old, "codexsdk/internal/schemaoracle/oracle_test.go"), oldTest)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"}} {
		if err := runGit(old, args...); err != nil {
			t.Fatal(err)
		}
	}
	next := filepath.Join(t.TempDir(), "new")
	if err := runGit(old, "clone", "--quiet", "--no-hardlinks", old, next); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(next, "codexsdk/internal/schemaoracle/oracle_test.go"), newTest)
	candidate := t.TempDir()
	writeFile(t, filepath.Join(candidate, "schema/Payload.json"), acceptanceTargetSchema)
	return old, next, candidate
}

// This executes only trusted literal fixture code. Proposed code exclusively uses
// runVerifierSourceGate's readonly no-network sandbox, never this test adapter.
func acceptanceLiteralGate(ctx context.Context, root, candidate, gate string, expected map[string]bool) (verifierProofLane, error) {
	lane := verifierProofLane{Started: true}
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-json", "./codexsdk/internal/schemaoracle")
	cmd.Dir = root
	cmd.Env = append(repairEnvironment(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	events := newVerifierSourceEvents(expected)
	var stderr repairOutput
	cmd.Stdout = events
	cmd.Stderr = &stderr
	err := cmd.Run()
	_, _ = events.baseline.diagnostic.Write([]byte(stderr.String()))
	lane.Diagnostic = boundedRepairDiagnostic(events.baseline.diagnostic.String())
	outcome := verifierSourceTestOutcome(events, err, stderr.String())
	lane.Passed = outcome == nil
	lane.Failed = outcome != nil
	return lane, outcome
}

func TestVerifierAcceptanceRealVersionPairingMatrix(t *testing.T) {
	for _, tt := range []struct {
		name, old, next string
		pass, red       bool
	}{
		{"pairing repaired", acceptanceMovingTest, acceptanceFixedTest, true, true},
		{"pairing unchanged", acceptanceMovingTest, acceptanceMovingTest, false, false},
		{"deleted old assertion", acceptanceMovingTest, `package schemaoracle;import "testing";func TestReplacement(t *testing.T){}`, false, false},
		{"skipped old assertion", acceptanceMovingTest, `package schemaoracle;import "testing";func TestContract(t *testing.T){t.Skip("removed")}`, false, false},
		{"already correct", acceptanceFixedTest, acceptanceFixedTest, true, false},
		{"old crash", strings.Replace(acceptanceMovingTest, `t.Fatal("schema and frozen fixture versions differ")`, `panic("unexpected crash")`, 1), acceptanceFixedTest, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			old, next, candidate := acceptanceFixture(t, tt.old, tt.next)
			before, _ := repairPatch(next)
			sourceBefore, err := verifierAcceptanceSchemaDigest(candidate)
			if err != nil {
				t.Fatal(err)
			}
			lanes, red, err := verifierAcceptanceWithGate(context.Background(), old, next, candidate, acceptanceLiteralGate)
			if (err == nil) != tt.pass || red != tt.red {
				t.Fatalf("red=%t pass=%t want=%t/%t lanes=%+v err=%v", red, err == nil, tt.red, tt.pass, lanes, err)
			}
			if tt.pass && len(lanes) != 4 {
				t.Fatalf("incomplete matrix: %+v", lanes)
			}
			after, _ := repairPatch(next)
			if repairDigest(before) != repairDigest(after) {
				t.Fatal("modified proposed tree")
			}
			if err := AssertClean(old); err != nil {
				t.Fatal("modified accepted tree", err)
			}
			sourceAfter, err := verifierAcceptanceSchemaDigest(candidate)
			if err != nil || sourceAfter != sourceBefore {
				t.Fatal("modified immutable candidate")
			}
		})
	}
}

func TestVerifierAcceptanceSourceViewBoundaries(t *testing.T) {
	for _, mode := range []string{"file-symlink", "directory-symlink", "root-symlink", "non-json", "metadata", "empty"} {
		t.Run(mode, func(t *testing.T) {
			candidate := t.TempDir()
			schema := filepath.Join(candidate, "schema")
			if err := os.Mkdir(schema, 0700); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "file-symlink":
				if err := os.Symlink("/etc/passwd", filepath.Join(schema, "Payload.json")); err != nil {
					t.Fatal(err)
				}
			case "directory-symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(schema, "nested")); err != nil {
					t.Fatal(err)
				}
			case "root-symlink":
				if err := os.Remove(schema); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), schema); err != nil {
					t.Fatal(err)
				}
			case "non-json":
				writeFile(t, filepath.Join(schema, "injected.go"), "package attack")
			case "metadata":
				writeFile(t, filepath.Join(schema, "manifest.json"), "{}")
			}
			if _, err := verifierAcceptanceSchemaDigest(candidate); err == nil {
				t.Fatal("unsafe schema source accepted")
			}
		})
	}
	root := t.TempDir()
	candidate := t.TempDir()
	for _, file := range []string{"baseline.go", "manifest.json", "baseline_metadata.json"} {
		writeFile(t, filepath.Join(root, mechanicalPrefix, file), "retained")
	}
	writeFile(t, filepath.Join(root, mechanicalPrefix, "Removed.json"), "{}")
	writeFile(t, filepath.Join(candidate, "schema/nested/Added.json"), `{"type":"boolean"}`)
	before, err := verifierAcceptanceSchemaDigest(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifierAcceptanceTarget(root, candidate); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"baseline.go", "manifest.json", "baseline_metadata.json"} {
		if b, err := os.ReadFile(filepath.Join(root, mechanicalPrefix, file)); err != nil || string(b) != "retained" {
			t.Fatal("changed non-schema accepted source", file)
		}
	}
	if _, err := os.Stat(filepath.Join(root, mechanicalPrefix, "Removed.json")); !os.IsNotExist(err) {
		t.Fatal("stale schema retained")
	}
	if b, err := os.ReadFile(filepath.Join(root, mechanicalPrefix, "nested/Added.json")); err != nil || string(b) != `{"type":"boolean"}` {
		t.Fatal("target bytes missing")
	}
	writeFile(t, filepath.Join(candidate, "schema/Other.json"), `{"type":"string"}`)
	after, err := verifierAcceptanceSchemaDigest(candidate)
	if err != nil || before == after {
		t.Fatal("full target view not digest-bound")
	}
}

func TestVerifierAcceptanceUnknownFailureCannotSupplyOldRed(t *testing.T) {
	old, next, candidate := acceptanceFixture(t, acceptanceMovingTest, acceptanceFixedTest)
	calls := 0
	_, red, err := verifierAcceptanceWithGate(context.Background(), old, next, candidate, func(context.Context, string, string, string, map[string]bool) (verifierProofLane, error) {
		calls++
		if calls == 2 {
			return verifierProofLane{Failed: true}, verifierFeedback(errors.New("compile failed"))
		}
		return verifierProofLane{Passed: true}, nil
	})
	if err == nil || red || calls != 2 {
		t.Fatalf("unknown supplied old red: %t %v calls=%d", red, err, calls)
	}
}

// Required CI calls this through the existing TestLiveVerifierProofMatrix prefix.
// No local namespace fallback or unsandboxed proposed-code execution is allowed.
func TestLiveVerifierProofMatrixAcceptancePairing(t *testing.T) {
	if os.Getenv("LLMGO_REPAIR_SANDBOX_TEST") != "1" {
		t.Skip("explicit hosted sandbox proof only")
	}
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "correctly paired", true: "deleted assertion"}[deleted], func(t *testing.T) {
			nextTest := acceptanceFixedTest
			if deleted {
				nextTest = `package schemaoracle;import "testing";func TestReplacement(t *testing.T){}`
			}
			old, next, candidate := acceptanceFixture(t, acceptanceMovingTest, nextTest)
			lanes, red, err := runVerifierAcceptance(context.Background(), old, next, candidate)
			if deleted {
				if err == nil {
					t.Fatal("deleted permanent assertion accepted")
				}
			} else if err != nil || !red || len(lanes) != 4 {
				t.Fatalf("pairing proof missing: red=%t lanes=%+v err=%v", red, lanes, err)
			}
		})
	}
}

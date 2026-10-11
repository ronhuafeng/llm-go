package protocolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/codexexec"
)

func TestOracleFailureSurvivesRealSuccessFloodAndRepairCheckpoint(t *testing.T) {
	const pkg = "github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe"
	const cause = "FIRST_ASSERTION_CAUSE required record field"
	const secret = "private-oracle-test-secret-938247"
	t.Setenv("ORACLE_TEST_SECRET", secret)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module "+pkg+"\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "contract_test.go"), `package fixture
import("testing";"fmt";"strings";"os")
func TestTrustedSchemaOracle(t *testing.T){
 t.Run("earlier_success",func(t *testing.T){t.Log(strings.Repeat("SUCCESS_PAYLOAD",10000))})
 t.Run("early_failure",func(t *testing.T){t.Log(strings.Repeat("preceding failure-local log ",10000));t.Fatalf("FIRST_ASSERTION_CAUSE required record field token=%s",os.Getenv("ORACLE_TEST_SECRET"))})
 for i:=0;i<160;i++{t.Run(fmt.Sprintf("later_success_%03d",i),func(t *testing.T){t.Log(strings.Repeat("SUCCESS_PAYLOAD",500))})}
}
`)
	cmd := exec.Command("go", "test", "-count=1", "-json", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err == nil {
		t.Fatal("failing fixture passed")
	}
	if len(out) < 65536 {
		t.Fatalf("fixture lacks truncation pressure: %d", len(out))
	}
	var target oracleTestEvents
	baseline := baselineTestEvents{expected: map[string]bool{pkg + "/TestTrustedSchemaOracle": true}, ran: map[string]bool{}, passed: map[string]bool{}}
	var legacy repairOutput
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var e struct{ Action, Output string }
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatal(err)
		}
		if e.Action == "output" || e.Action == "build-output" {
			legacy.Write([]byte(e.Output))
		}
		framed := append(append([]byte(nil), line...), '\n')
		target.Write(framed)
		baseline.Write(framed)
	}
	if strings.Contains(boundedRepairDiagnostic(legacy.String()), cause) {
		t.Fatal("fixture did not reproduce old lost-cause bug")
	}
	if !target.started || !target.failed || target.passed || baseline.verify() == nil {
		t.Fatal("failing evidence accepted")
	}
	for name, text := range map[string]string{"target": target.diagnostic.String(), "baseline": baseline.diagnostic.String()} {
		retained := boundedRepairDiagnostic("outer repair: " + boundedRepairDiagnostic("inner oracle: "+text))
		if !strings.Contains(retained, cause) || !strings.Contains(retained, "early_failure") || !strings.Contains(retained, "contract_test.go:") {
			t.Fatalf("%s lost source cause: %s", name, retained)
		}
		if strings.Contains(retained, "SUCCESS_PAYLOAD") || strings.Contains(retained, secret) || !strings.Contains(retained, "[redacted]") {
			t.Fatalf("%s leaked successful noise or secret: %s", name, retained)
		}
		if len(retained) > 8192 {
			t.Fatalf("retained bound %d", len(retained))
		}
	}
	req := repairFixture(t)
	req.MaxRounds = 2
	round := 0
	req.Executor = repairExecutorFixture{run: func(_ context.Context, prompt, session string) (codexexec.Result, error) {
		round++
		if round > 1 && (!strings.Contains(prompt, cause) || strings.Contains(prompt, secret)) {
			t.Fatalf("repair feedback lost cause or secret: %s", prompt)
		}
		writeFile(t, filepath.Join(req.RepoRoot, "codexsdk/internal/protocolgen/fixture.go"), "package protocolgen // same proposal\n")
		return codexexec.Result{Status: "completed", SessionID: "fixture-session"}, nil
	}}
	req.Oracle = func(context.Context, ResumeRequest) (SyncResult, []byte, error) {
		return SyncResult{Stage: "verification"}, nil, fmt.Errorf("independent schema contract: %s", boundedRepairDiagnostic(target.diagnostic.String()))
	}
	_, _ = Repair(context.Background(), req)
	cp := readRepairCheckpoint(t, req.CheckpointDir)
	if round != 2 || !strings.Contains(cp.Diagnostic, cause) || strings.Contains(cp.Diagnostic, secret) || len(cp.Diagnostic) > 8192 {
		t.Fatalf("round=%d checkpoint=%+v", round, cp)
	}
}

func TestOracleDiagnosticKeepsBoundedUnfinishedAndRootContext(t *testing.T) {
	var d oracleDiagnostic
	d.event("output", "pkg", "TestRoot", "panic: ROOT_CRASH_CAUSE\n")
	if !strings.Contains(d.String(), "unfinished test output pkg/TestRoot") || !strings.Contains(d.String(), "ROOT_CRASH_CAUSE") {
		t.Fatal(d.String())
	}
	d = oracleDiagnostic{}
	d.Write([]byte("NO_TEST_SETUP_CAUSE\n"))
	d.Write([]byte(strings.Repeat("runtime output ", 10000)))
	if !strings.Contains(d.String(), "NO_TEST_SETUP_CAUSE") || len(d.String()) > 1024 {
		t.Fatal("lost coarse process cause")
	}
	d = oracleDiagnostic{}
	for i := 0; i < 1000; i++ {
		d.event("output", "pkg", fmt.Sprintf("TestParallel%d", i), strings.Repeat("payload", 1000))
	}
	if len(d.tests) > 12 {
		t.Fatal("unbounded active tests")
	}
	d.event("fail", "pkg", "TestParallel999", "")
	if !strings.Contains(d.String(), "failed without captured assertion output") {
		t.Fatal("missing explicit overflow fallback")
	}
	if len(d.String()) > 6144 {
		t.Fatal("rendered budget increased")
	}
}

func TestOracleRealProcessFailuresKeepContextWithoutTestVerdict(t *testing.T) {
	const pkg = "github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracleprobe"
	for _, tc := range []struct{ name, source, cause string }{
		{"setup", `package fixture
import("testing";"fmt";"os")
func TestMain(m *testing.M){fmt.Println("SETUP_ROOT_CAUSE");os.Exit(2)}
func TestTrustedSchemaOracle(t *testing.T){}`, "SETUP_ROOT_CAUSE"},
		{"unfinished", `package fixture
import("testing";"os";"strings")
func TestTrustedSchemaOracle(t *testing.T){t.Log(strings.Repeat("earlier unfinished context ",10000));t.Log("UNFINISHED_ROOT_CAUSE");os.Exit(2)}`, "UNFINISHED_ROOT_CAUSE"},
		{"panic", `package fixture
import("testing";"strings")
func TestTrustedSchemaOracle(t *testing.T){t.Log(strings.Repeat("earlier panic context ",10000));panic("PANIC_ROOT_CAUSE")}`, "PANIC_ROOT_CAUSE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "go.mod"), "module "+pkg+"\n\ngo 1.26\n")
			writeFile(t, filepath.Join(dir, "contract_test.go"), tc.source)
			cmd := exec.Command("go", "test", "-json", ".")
			cmd.Dir = dir
			out, err := cmd.Output()
			if err == nil {
				t.Fatal("process unexpectedly passed")
			}
			var target oracleTestEvents
			target.Write(out)
			baseline := baselineTestEvents{expected: map[string]bool{pkg + "/TestTrustedSchemaOracle": true}, ran: map[string]bool{}, passed: map[string]bool{}}
			baseline.Write(out)
			if target.passed || baseline.verify() == nil {
				t.Fatal("crash supplied proof")
			}
			for _, text := range []string{target.diagnostic.String(), baseline.diagnostic.String()} {
				if !strings.Contains(boundedRepairDiagnostic(text), tc.cause) {
					t.Fatalf("lost process cause: %s; raw=%s", text, out)
				}
			}
		})
	}
}

func TestOracleDiagnosticSeparatesBuildAndTestContexts(t *testing.T) {
	var d oracleDiagnostic
	d.event("build-output", "successful/build", "", strings.Repeat("successful compiler noise", 1000))
	d.event("build-output", "broken/build", "", "broken.go:4: undefined: MISSING_BINDING\n")
	d.event("build-fail", "broken/build", "", "")
	for i := 0; i < 100; i++ {
		d.event("output", "pkg", "TestPassed", "passing payload\n")
		d.event("pass", "pkg", "TestPassed", "")
	}
	if text := d.String(); !strings.Contains(text, "undefined: MISSING_BINDING") || strings.Contains(text, "successful compiler noise") {
		t.Fatal(text)
	}
	d = oracleDiagnostic{}
	d.event("output", "pkg", "TestParent/child", "=== RUN TestParent/child\nsource_test.go:3: ASSERTION_WITH_STATUS_PREFIX\n")
	d.event("fail", "pkg", "TestParent/child", "")
	d.event("fail", "pkg", "TestParent", "")
	d.Write([]byte(strings.Repeat("late stderr ", 10000)))
	if text := boundedRepairDiagnostic(d.String()); !strings.Contains(text, "ASSERTION_WITH_STATUS_PREFIX") || !strings.Contains(text, "TestParent/child") {
		t.Fatal(text)
	}
	d = oracleDiagnostic{}
	d.event("output", "pkg", strings.Repeat("long", 200), "lost payload")
	d.event("fail", "pkg", strings.Repeat("long", 200), "")
	if !strings.Contains(d.String(), "diagnostic output omitted: active-test/key bound") {
		t.Fatal("missing key omission notice")
	}
}

func TestOracleMalformedOutputRetainsCoarseFailure(t *testing.T) {
	var target oracleTestEvents
	baseline := baselineTestEvents{expected: map[string]bool{}, ran: map[string]bool{}, passed: map[string]bool{}}
	for _, write := range []func([]byte) (int, error){target.Write, baseline.Write} {
		write([]byte("launcher: UNKNOWN_ROOT_CAUSE\n"))
	}
	if !target.invalid || baseline.verify() == nil {
		t.Fatal("malformed output supplied proof")
	}
	for _, text := range []string{target.diagnostic.String(), baseline.diagnostic.String()} {
		if !strings.Contains(text, "unparsed go test output: launcher: UNKNOWN_ROOT_CAUSE") {
			t.Fatal(text)
		}
	}
}

func TestOracleUnfinishedOmissionAndIncompleteFramingAreExplicit(t *testing.T) {
	var d oracleDiagnostic
	d.event("output", "pkg", "TestA", strings.Repeat("context", 1000))
	d.event("output", "pkg", "TestB", "unfinished later context")
	if !strings.Contains(d.String(), "additional unfinished-test output omitted") {
		t.Fatal("unfinished omission not disclosed")
	}
	var target oracleTestEvents
	baseline := baselineTestEvents{expected: map[string]bool{}, ran: map[string]bool{}, passed: map[string]bool{}}
	fragment := []byte(`{"Action":"output","Output":"INCOMPLETE_ROOT_CAUSE`)
	target.Write(fragment)
	baseline.Write(fragment)
	if target.pending == "" || baseline.verify() == nil || target.passed {
		t.Fatal("incomplete JSON supplied proof")
	}
	// The process-finalization path preserves this still-invalid fragment as
	// coarse context; it is never parsed into a synthetic test verdict.
	target.diagnostic.Write([]byte("incomplete go test output: " + target.pending))
	if !strings.Contains(target.diagnostic.String(), "INCOMPLETE_ROOT_CAUSE") {
		t.Fatal("incomplete context lost")
	}
}

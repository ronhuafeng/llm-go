package protocolsync

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

// Uses production expectation framing, the compiled factory harness, and the
// production process checker. No test encoder supplies either side's verdict.
func decoderFixture(t *testing.T, req schemaoracle.Request, code string) (error, string) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module fixture\ngo 1.26.8\n")
	writeFile(t, filepath.Join(dir, "subject/payload.go"), code)
	req.ImportPath = "fixture/subject"
	source, err := schemaoracle.Harness(req)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "probe/main.go"), string(source))
	binary := filepath.Join(dir, "decoder")
	cmd := exec.Command("go", "build", "-o", binary, "./probe")
	cmd.Dir = dir
	var diagnostics bytes.Buffer
	if out, err := oracleCommandOutput(&diagnostics, cmd); err != nil {
		return err, string(out)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req.Context = ctx
	go func() { err := schemaoracle.Write(req, writer); writer.CloseWithError(err); done <- err }()
	stream := bufio.NewReader(reader)
	var header schemaoracle.Header
	err = schemaoracle.ReadFrame(stream, &header)
	if err == nil {
		err = checkDecoderProcess(ctx, exec.CommandContext(ctx, binary), header, stream, &diagnostics)
	}
	reader.Close()
	generationErr := <-done
	if err == nil {
		err = generationErr
	}
	return err, diagnostics.String()
}

func TestDecoderExecutionDefectsAndNativeFailures(t *testing.T) {
	req := schemaoracle.Request{Schemas: map[string][]byte{"Item.json": []byte(`{"type":"object","properties":{"item":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}},"required":["item"]}`)}, Bindings: map[string]string{"Item.json": "Payload"}, CloseObjects: true}
	const code = `package subject
import("encoding/json";"fmt")
type Payload struct{Item json.RawMessage ` + "`json:\"item\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var root map[string]json.RawMessage;if json.Unmarshal(b,&root)!=nil||len(root)!=1||root["item"]==nil{return fmt.Errorf("item required")};item:=root["item"];CHECK;var object map[string]any;if json.Unmarshal(item,&object)!=nil||len(object)!=1{return fmt.Errorf("item object")};if _,ok:=object["id"].(string);!ok{return fmt.Errorf("id")};p.Item=item;return nil}
`
	good := strings.Replace(code, "CHECK", "", 1)
	for _, tc := range []struct {
		name, code, cause string
		pass              bool
	}{
		{"correct", good, "", true},
		{"accept-invalid", strings.Replace(code, "CHECK", `if string(item)=="null"||string(item)=="false"||string(item)=="{}"{p.Item=item;return nil}`, 1), "schema-invalid input accepted", false},
		{"reject-valid", strings.Replace(code, "CHECK", `return fmt.Errorf("deliberate valid rejection")`, 1), "schema-valid input rejected", false},
		{"compile", good + "\nvar _ = missingOracleBinding", "undefined: missingOracleBinding", false},
		{"panic", strings.Replace(good, "var root", `panic("ORACLE_WITNESS_PANIC");var root`, 1), "ORACLE_WITNESS_PANIC", false},
		{"early clean exit", strings.Replace(good, `"fmt"`, `"fmt";"os"`, 1) + "\nfunc init(){os.Exit(0)}", "EOF", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err, out := decoderFixture(t, req, tc.code)
			if (err == nil) != tc.pass {
				t.Fatalf("process=%v %s", err, out)
			}
			if tc.cause != "" && !strings.Contains(out+err.Error(), tc.cause) {
				t.Fatalf("lost native cause %s: %v %s", tc.cause, err, out)
			}
			if tc.name == "accept-invalid" && strings.Count(out, `"Kind":"schema_contract_failure"`) != 3 {
				t.Fatalf("independent witnesses suppressed: %s", out)
			}
		})
	}
}

func TestDirectDiagnosticsKeepLateSourceAndObservedDifference(t *testing.T) {
	const secret = "private-oracle-url-and-body-28473"
	t.Setenv("ORACLE_BODY_SECRET", secret)
	padding := strings.Repeat("padding-", 600) + "https://user:" + secret + "@example.test/?token=" + secret
	for _, roundtrip := range []bool{false, true} {
		t.Run(strconv.FormatBool(roundtrip), func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{"aaa": map[string]any{"const": padding}, "zzz": map[string]any{"type": "string", "minLength": 1}}, "required": []string{"aaa", "zzz"}})
			req := schemaoracle.Request{Schemas: map[string][]byte{"Late.json": raw}, Bindings: map[string]string{"Late.json": "Payload"}, CloseObjects: true}
			code := `package subject
import("encoding/json";"fmt")
type Payload struct{AAA string ` + "`json:\"aaa\"`" + `;ZZZ string ` + "`json:\"zzz\"`" + `}
func(p *Payload)UnmarshalJSON(b []byte)error{var m map[string]any;if json.Unmarshal(b,&m)!=nil||len(m)!=2{return fmt.Errorf("fields")};a,ok:=m["aaa"].(string);if !ok||a!=` + strconv.Quote(padding) + `{return fmt.Errorf("aaa")};z,ok:=m["zzz"].(string);if !ok{ return fmt.Errorf("zzz")};CHECK;p.AAA=a;p.ZZZ=z;return nil}
`
			check := ""
			if roundtrip {
				check = `if z==""{return fmt.Errorf("empty")};z="LATE_OBSERVED_DIFFERENCE"`
			}
			err, out := decoderFixture(t, req, strings.Replace(code, "CHECK", check, 1))
			if err == nil || !strings.Contains(out, padding) {
				t.Fatalf("incomplete evidence: %v %s", err, out)
			}
			bounded := boundedRepairDiagnostic(err.Error() + "\n" + out)
			if len(bounded) > 8192 || strings.Contains(bounded, secret) || !strings.Contains(bounded, "[redacted]") {
				t.Fatal("bounded native feedback leaked or exceeded its limit")
			}

			if roundtrip {
				if !strings.Contains(out, "LATE_OBSERVED_DIFFERENCE") || !strings.Contains(err.Error(), "#/zzz") {
					t.Fatal(err, out)
				}
			} else if !strings.Contains(out, "Late.json#/properties/zzz/minLength") {
				t.Fatal("source attribution lost", out)
			}
		})
	}
}

func TestDecoderProcessCancellationReaps(t *testing.T) {
	req := schemaoracle.Request{Schemas: map[string][]byte{"P.json": []byte(`{"type":"boolean"}`)}, Bindings: map[string]string{"P.json": "Payload"}}
	var wire bytes.Buffer
	if err := schemaoracle.Write(req, &wire); err != nil {
		t.Fatal(err)
	}
	stream := bufio.NewReader(&wire)
	var header schemaoracle.Header
	if err := schemaoracle.ReadFrame(stream, &header); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sleep", "30")
	start := time.Now()
	err := checkDecoderProcess(ctx, cmd, header, stream, io.Discard)
	if err == nil || time.Since(start) > 3*time.Second || cmd.ProcessState == nil {
		t.Fatalf("cleanup failed: %v %v", err, cmd.ProcessState)
	}
}

func TestDecoderObservationProtocolFailsClosed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module fixture\ngo 1.26.8\n")
	writeFile(t, filepath.Join(dir, "main.go"), `package main
import("bufio";"encoding/json";"fmt";"os";"time")
type request struct{ID int;Type,Mode,Input string}
type observation struct{ID int;Mode string;Accepted bool;Output,Error string;Fields []string;Struct bool}
func main(){mode:=os.Args[1];if mode=="exit"{return};if mode=="panic"{panic("native observation panic")};if mode=="incomplete"{fmt.Print("{");return};if mode=="skip"{for{time.Sleep(time.Hour)}};if mode=="trailing"{defer fmt.Println("{}")};scanner:=bufio.NewScanner(os.Stdin);enc:=json.NewEncoder(os.Stdout);for scanner.Scan(){var q request;if json.Unmarshal(scanner.Bytes(),&q)!=nil{return};if mode=="missing"&&q.Mode=="decode"{return};r:=observation{ID:q.ID,Mode:q.Mode,Fields:[]string{}};if q.Mode=="decode"{r.Accepted=q.Input=="true"||q.Input=="false";if r.Accepted{r.Output=q.Input}else{r.Error="invalid"}};if mode=="id"{r.ID++};if mode=="malformed"{fmt.Println("{} {}");return};enc.Encode(r);if mode=="duplicate"{enc.Encode(r)}}}
`)
	binary := filepath.Join(dir, "observer")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native fixture: %v %s", err, out)
	}
	req := schemaoracle.Request{Schemas: map[string][]byte{"P.json": []byte(`{"type":"boolean"}`)}, Bindings: map[string]string{"P.json": "Payload"}}
	for _, mode := range []string{"good", "exit", "panic", "incomplete", "skip", "missing", "trailing", "id", "malformed", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			var wire bytes.Buffer
			if err := schemaoracle.Write(req, &wire); err != nil {
				t.Fatal(err)
			}
			stream := bufio.NewReader(&wire)
			var header schemaoracle.Header
			if err := schemaoracle.ReadFrame(stream, &header); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, mode)
			var diagnostic bytes.Buffer
			err := checkDecoderProcess(ctx, cmd, header, stream, &diagnostic)
			if (err == nil) != (mode == "good") {
				t.Fatalf("mode=%s err=%v %s", mode, err, diagnostic.Bytes())
			}
			if cmd.ProcessState == nil {
				t.Fatal("process was not reaped")
			}
		})
	}
}

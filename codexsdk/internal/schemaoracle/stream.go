package schemaoracle

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/format"
	"go/token"
	"io"
	"math/big"
	"reflect"
	"strconv"
	"strings"
)

const (
	maxValueBytes         = 16 << 20
	maxFrameBytes         = 128 << 20 // JSON string escaping can expand a wire value sixfold.
	maxProbes             = 200000
	maxTargetProbes       = 40000
	maxWorkBytes    int64 = 16 << 30 // Serialized input work, not retained memory.
)

// Header binds the incremental expectations to exact source bytes and factories.
// It carries no witness values or candidate executable code.
type Header struct {
	Bindings map[string]string
	Sources  map[string]string
}

type record struct {
	Target *Target `json:",omitempty"`
	Probe  *Probe  `json:",omitempty"`
	Done   bool    `json:",omitempty"`
	Count  int     `json:",omitempty"`
}

type decoderInput struct {
	ID                int
	Type, Mode, Input string
}
type observation struct {
	ID            int
	Mode          string
	Accepted      *bool
	Output, Error string
	Fields        []string
	Struct        *bool
}

// ReadFrame requires one complete bounded JSON line, including its delimiter.
func ReadFrame(r *bufio.Reader, value any) error {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > maxFrameBytes {
			return fmt.Errorf("schema oracle: framing bound exceeded")
		}
		line = append(line, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			if len(line) > 0 {
				return fmt.Errorf("schema oracle: incomplete frame: %w", err)
			}
			return err
		}
		d := json.NewDecoder(bytes.NewReader(line))
		d.DisallowUnknownFields()
		if err = d.Decode(value); err != nil {
			return fmt.Errorf("schema oracle: invalid frame: %w", err)
		}
		if err = d.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("schema oracle: trailing frame data")
		}
		return nil
	}
}

func writeFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b)+1 > maxFrameBytes {
		return fmt.Errorf("schema oracle: framing bound exceeded")
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func SourceDigests(schemas map[string][]byte) map[string]string {
	result := map[string]string{}
	for name, raw := range schemas {
		sum := sha256.Sum256(raw)
		result[name] = hex.EncodeToString(sum[:])
	}
	return result
}

// Write derives each expectation in its owning process, before requesting any
// candidate observation for that input. No complete corpus is retained or sent.
func Write(req Request, out io.Writer) error {
	if _, err := Harness(req); err != nil {
		return err
	}
	if err := writeFrame(out, Header{req.Bindings, SourceDigests(req.Schemas)}); err != nil {
		return err
	}
	count := 0
	if err := Walk(req, func(t Target, p *Probe) error {
		if p == nil {
			return writeFrame(out, record{Target: &t})
		}
		count++
		return writeFrame(out, record{Probe: p})
	}); err != nil {
		return err
	}
	return writeFrame(out, record{Done: true, Count: count})
}

// Harness generates only independently selected type factories and a stdlib
// decoder loop. It receives no expected verdicts. Compile it once, separately
// from candidate tests, so TestMain cannot suppress execution.
func Harness(req Request) ([]byte, error) {
	if req.Context != nil {
		if err := req.Context.Err(); err != nil {
			return nil, err
		}
	}
	if len(req.Bindings) == 0 || len(req.Bindings) > 2048 {
		return nil, fmt.Errorf("schema oracle: target count outside 1..2048")
	}
	var b strings.Builder
	b.WriteString("package main\nimport(\"bufio\";\"bytes\";\"encoding/json\";\"fmt\";\"io\";\"os\";\"reflect\";\"sort\";\"strings\")\n")
	prefix := ""
	if req.ImportPath != "" {
		fmt.Fprintf(&b, "import subject %s\n", strconv.Quote(req.ImportPath))
		prefix = "subject."
	}
	b.WriteString("func factory(name string)any{switch name{\n")
	for _, schema := range keys(req.Bindings) {
		name := req.Bindings[schema]
		if !token.IsIdentifier(name) || token.Lookup(name).IsKeyword() {
			return nil, fmt.Errorf("schema oracle %s: invalid Go binding %q", schema, name)
		}
		fmt.Fprintf(&b, "case %s:return new(%s%s)\n", strconv.Quote(schema), prefix, name)
	}
	b.WriteString("};panic(\"unknown schema factory\")}\n")
	fmt.Fprintf(&b, "const maxFrameBytes=%d\nconst maxValueBytes=%d\n", maxFrameBytes, maxValueBytes)
	b.WriteString(`
type input struct{ID int;Type,Mode,Input string}
type observation struct{ID int;Mode string;Accepted bool;Output,Error string;Fields []string;Struct bool}
func main(){if err:=run();err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}}
func run()error{
 reader:=bufio.NewReader(os.Stdin)
 for {
  var line []byte
  for {p,e:=reader.ReadSlice('\n');if len(line)+len(p)>maxFrameBytes{return fmt.Errorf("decoder frame bound exceeded")};line=append(line,p...);if e==bufio.ErrBufferFull{continue};if e==io.EOF&&len(line)==0{return nil};if e!=nil{return e};break}
  var q input;d:=json.NewDecoder(bytes.NewReader(line));d.DisallowUnknownFields();if e:=d.Decode(&q);e!=nil{return e};if e:=d.Decode(new(any));e!=io.EOF{return fmt.Errorf("trailing decoder input")}
  if len(q.Input)>maxValueBytes{return fmt.Errorf("decoder value bound exceeded")}
  v:=factory(q.Type);r:=observation{ID:q.ID,Mode:q.Mode}
  switch q.Mode{
  case "fields":typ:=reflect.TypeOf(v).Elem();r.Struct=typ.Kind()==reflect.Struct;r.Fields=[]string{};if r.Struct{for i:=0;i<typ.NumField();i++{f:=typ.Field(i);if f.PkgPath!=""{continue};tag:=strings.Split(f.Tag.Get("json"),",")[0];if tag=="-"{continue};if tag==""{tag=f.Name};r.Fields=append(r.Fields,tag)}};sort.Strings(r.Fields)
  case "decode":
   // Converting the immutable string gives each decoder fresh owned input.
   if e:=json.Unmarshal([]byte(q.Input),v);e!=nil{r.Error=e.Error()}else{r.Accepted=true;b,e:=json.Marshal(v);if e!=nil{r.Error=e.Error()}else{if len(b)>maxValueBytes{return fmt.Errorf("decoder output bound exceeded")};r.Output=string(b)}}
  default:return fmt.Errorf("unknown decoder mode")
  }
  encoded,e:=json.Marshal(r);if e!=nil{return e};if len(encoded)+1>maxFrameBytes{return fmt.Errorf("decoder output frame bound exceeded")};if _,e=os.Stdout.Write(append(encoded,'\n'));e!=nil{return e}
 }
}
`)
	if b.Len() > 1<<20 {
		return nil, fmt.Errorf("schema oracle: factory source bound exceeded")
	}
	return format.Source([]byte(b.String()))
}

// Check compares observations itself; decoder success text is never a verdict.
// All attributable failures are retained, or diagnostics are explicitly incomplete.
// Closing input and requiring EOF detects duplicate/trailing observations.
func Check(ctx context.Context, header Header, expectations *bufio.Reader, input io.WriteCloser, output io.Reader, diagnostics io.Writer) error {
	if len(header.Bindings) == 0 || len(header.Bindings) > 2048 {
		return fmt.Errorf("schema oracle: coverage empty or target bound exceeded")
	}
	observations := bufio.NewReader(output)
	seen := map[string]bool{}
	var target Target
	count, index, valid, failures, diagnosticBytes, sequence := 0, 0, 0, 0, 0, 0
	var summary strings.Builder
	var workBytes int64
	fail := func(p *Probe, actual observation, reason string) error {
		failures++
		if summary.Len() < 3000 {
			fmt.Fprintf(&summary, "%s probe_%d: %.600s\n", target.Schema, index, reason)
		}
		b, err := json.Marshal(struct {
			Kind, Trust, Schema string
			Index               int
			Expected            *Probe   `json:",omitempty"`
			Fields              []string `json:",omitempty"`
			Actual              observation
			Reason              string
		}{"schema_contract_failure", "source-derived", target.Schema, index, p, target.Fields, actual, reason})
		if err != nil {
			return err
		}
		diagnosticBytes += len(b) + 1
		if diagnosticBytes > 16<<20 {
			return fmt.Errorf("schema oracle: failure diagnostics incomplete: 16 MiB bound exceeded")
		}
		if diagnostics != nil {
			_, err = diagnostics.Write(append(b, '\n'))
		}
		return err
	}
	exchange := func(q decoderInput) (observation, error) {
		sequence++
		q.ID = sequence
		if err := writeFrame(input, q); err != nil {
			return observation{}, err
		}
		var actual observation
		if err := ReadFrame(observations, &actual); err != nil {
			return actual, err
		}
		if actual.Accepted == nil || actual.Struct == nil {
			return actual, fmt.Errorf("incomplete decoder observation")
		}
		if actual.ID != q.ID || actual.Mode != q.Mode {
			return actual, fmt.Errorf("missing, duplicate or out-of-order decoder observation: got %d/%s want %d/%s", actual.ID, actual.Mode, q.ID, q.Mode)
		}
		return actual, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var r record
		if err := ReadFrame(expectations, &r); err != nil {
			return fmt.Errorf("schema oracle: incomplete expectations: %w", err)
		}
		switch {
		case r.Target != nil && r.Probe == nil && !r.Done && r.Count == 0:
			if target.Schema != "" && valid == 0 {
				return fmt.Errorf("schema oracle: target %s has no valid obligations", target.Schema)
			}
			target = *r.Target
			index = 0
			valid = 0
			if target.Schema == "" || seen[target.Schema] || header.Bindings[target.Schema] != target.GoType {
				return fmt.Errorf("schema oracle: unknown, duplicate or misbound target %s", target.Schema)
			}
			seen[target.Schema] = true
			actual, err := exchange(decoderInput{Type: target.Schema, Mode: "fields"})
			if err != nil {
				if evidenceErr := fail(nil, actual, "decoder observation unavailable: "+err.Error()); evidenceErr != nil {
					return fmt.Errorf("%w; failure evidence: %v", err, evidenceErr)
				}
				return err
			}
			if target.CheckFields && (!*actual.Struct || !reflect.DeepEqual(actual.Fields, target.Fields)) {
				if err = fail(nil, actual, fmt.Sprintf("schema fields=%v Go JSON fields=%v", target.Fields, actual.Fields)); err != nil {
					return err
				}
			}
		case r.Target == nil && r.Probe != nil && !r.Done && r.Count == 0:
			if target.Schema == "" {
				return fmt.Errorf("schema oracle: probe without target")
			}
			p := r.Probe
			count++
			index++
			workBytes += int64(len(p.JSON))
			if count > maxProbes || index > maxTargetProbes || workBytes > maxWorkBytes || len(p.JSON) > maxValueBytes {
				return fmt.Errorf("schema oracle: work/value bound exceeded")
			}
			if p.Valid {
				valid++
			}
			actual, err := exchange(decoderInput{Type: target.Schema, Mode: "decode", Input: p.JSON})
			if err != nil {
				if evidenceErr := fail(p, actual, "decoder observation unavailable: "+err.Error()); evidenceErr != nil {
					return fmt.Errorf("%w; failure evidence: %v", err, evidenceErr)
				}
				return err
			}
			reason := ""
			if !p.Valid && *actual.Accepted {
				reason = "schema-invalid input accepted: " + p.Diagnostic
			}
			if p.Valid && !*actual.Accepted {
				reason = "schema-valid input rejected: " + actual.Error
			}
			if p.Valid && *actual.Accepted {
				if actual.Error != "" {
					reason = "marshal failed: " + actual.Error
				} else if err = roundtrip(target, p.JSON, actual.Output); err != nil {
					reason = err.Error()
				}
			}
			if reason != "" {
				if err = fail(p, actual, reason); err != nil {
					return err
				}
			}
		case r.Target == nil && r.Probe == nil && r.Done:
			if r.Count != count || count == 0 || len(seen) != len(header.Bindings) || valid == 0 {
				return fmt.Errorf("schema oracle: incomplete target/probe coverage")
			}
			if err := ReadFrame(expectations, new(record)); err != io.EOF {
				return fmt.Errorf("schema oracle: trailing expectations: %v", err)
			}
			if err := input.Close(); err != nil {
				return err
			}
			if err := ReadFrame(observations, new(observation)); err != io.EOF {
				return fmt.Errorf("schema oracle: trailing decoder observations: %v", err)
			}
			if failures > 0 {
				return fmt.Errorf("schema oracle: %d contract failures:\n%s", failures, summary.String())
			}
			if diagnostics != nil {
				return writeFrame(diagnostics, map[string]any{"kind": "schema_contract_complete", "targets": len(seen), "probes": count})
			}
			return nil
		default:
			return fmt.Errorf("schema oracle: invalid expectation record")
		}
	}
}

func decodeWire(raw string) (any, error) {
	if len(raw) > maxValueBytes {
		return nil, fmt.Errorf("wire value bound exceeded")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing wire JSON")
	}
	return value, nil
}
func roundtrip(t Target, input, output string) error {
	expected, err := decodeWire(input)
	if err != nil {
		return err
	}
	actual, err := decodeWire(output)
	if err != nil {
		return fmt.Errorf("invalid roundtrip JSON: %w", err)
	}
	in, iok := expected.(map[string]any)
	out, ook := actual.(map[string]any)
	if iok && ook {
		params, present := in["params"]
		_, outputPresent := out["params"]
		method, mok := in["method"].(string)
		outputMethod, omok := out["method"].(string)
		if present && params == nil && !outputPresent && mok && omok && method == outputMethod {
			for _, allowed := range t.ParameterlessMethods {
				if method == allowed {
					delete(in, "params")
					break
				}
			}
		}
	}
	if path := difference(expected, actual, "#"); path != "" {
		return fmt.Errorf("wire value lost: path=%s", path)
	}
	return nil
}
func difference(a, b any, path string) string {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if ok {
			r, rok := new(big.Rat).SetString(string(x))
			s, sok := new(big.Rat).SetString(string(y))
			if rok && sok && r.Cmp(s) == 0 {
				return ""
			}
		}
	case map[string]any:
		y, ok := b.(map[string]any)
		if ok {
			names := map[string]bool{}
			for k := range x {
				names[k] = true
			}
			for k := range y {
				names[k] = true
			}
			for _, k := range keys(names) {
				v, present := y[k]
				_, exists := x[k]
				next := path + "/" + strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
				if !present || !exists {
					return next
				}
				if d := difference(x[k], v, next); d != "" {
					return d
				}
			}
			return ""
		}
	case []any:
		y, ok := b.([]any)
		if ok && len(x) == len(y) {
			for i, v := range x {
				if d := difference(v, y[i], fmt.Sprintf("%s/%d", path, i)); d != "" {
					return d
				}
			}
			return ""
		}
	default:
		if reflect.DeepEqual(a, b) {
			return ""
		}
	}
	return path
}

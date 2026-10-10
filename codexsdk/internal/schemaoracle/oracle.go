// Package schemaoracle freezes schema-derived wire probes in the trusted
// controller. It never imports the generator or inspects candidate runtime values.
package schemaoracle

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/format"
	"go/token"
	"io"
	"sort"
	"strconv"
	"strings"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// Request binds independently selected exact schema documents to Go identifiers.
// Bindings select independently determined changed targets, including optional
// #/definitions/... fragments. Schemas includes their full owning documents.
// Candidate declarations may supply names only, never select this target set.
// CloseObjects is the explicit public decoder overlay: objects with declared
// properties and no additionalProperties reject unknown fields. Unconstrained
// objects and explicitly open maps remain open.
type Request struct {
	Schemas      map[string][]byte
	Bindings     map[string]string
	Package      string
	ImportPath   string
	CloseObjects bool
}

const (
	maxCorpusBytes     = 128 << 20
	maxCompressedBytes = 8 << 20
)

type Probe struct {
	JSON  string
	Valid bool
}
type Target struct {
	Schema, GoType string
	Probes         []Probe
	Fields         []string
	CheckFields    bool
}

// Build computes all expectations before candidate code is compiled or run.
// Bounds are deliberate: unsupported schema drift is a concrete controller
// error, never silently reduced coverage or candidate self-certification.
func Build(req Request) ([]Target, error) {
	if len(req.Bindings) == 0 {
		return nil, fmt.Errorf("schema oracle: no target schemas")
	}
	if len(req.Bindings) > 2048 {
		return nil, fmt.Errorf("schema oracle: target count exceeds 2048")
	}
	var result []Target
	totalJSONBytes, totalProbes := 0, 0
	var budgetErr error
	for _, name := range keys(req.Bindings) {
		binding := req.Bindings[name]
		if !token.IsIdentifier(binding) || token.Lookup(binding).IsKeyword() {
			return nil, fmt.Errorf("schema oracle %s: missing or invalid Go binding %q", name, binding)
		}
		docName, fragment, _ := strings.Cut(name, "#")
		fragment = "#" + fragment
		raw, exists := req.Schemas[docName]
		if !exists {
			return nil, fmt.Errorf("schema oracle %s: missing schema document", name)
		}
		if len(raw) > 16<<20 {
			return nil, fmt.Errorf("schema oracle %s: document exceeds 16 MiB", name)
		}
		var doc any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			return nil, fmt.Errorf("schema oracle %s: %w", name, err)
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("schema oracle %s: trailing JSON data", name)
		}
		root, err := pointer(doc, fragment)
		if err != nil {
			return nil, fmt.Errorf("schema oracle %s: %w", name, err)
		}
		if err := prepareReachable(doc, root, fragment, req.CloseObjects, map[string]bool{}); err != nil {
			return nil, fmt.Errorf("schema oracle %s: %w", name, err)
		}
		c := validator.NewCompiler()
		c.UseLoader(denyLoader{})
		const url = "https://schema-oracle.invalid/target.json"
		if err := c.AddResource(url, doc); err != nil {
			return nil, fmt.Errorf("schema oracle %s: %w", name, err)
		}
		compiled, err := c.Compile(url + fragment)
		if err != nil {
			return nil, fmt.Errorf("schema oracle %s: %w", name, err)
		}
		w := witnesses{doc: doc}
		values, err := w.make(root, fragment, 0)
		if err != nil {
			return nil, fmt.Errorf("schema oracle %s: %w", name, err)
		}
		target := Target{Schema: name, GoType: binding}
		target.Fields, target.CheckFields, err = objectFields(doc, root, 0)
		if err != nil {
			return nil, fmt.Errorf("schema oracle %s: %w", name, err)
		}
		seen := map[string]bool{}
		add := func(v any) {
			if budgetErr != nil {
				return
			}
			b, _ := json.Marshal(v)
			s := string(b)
			if !seen[s] {
				totalJSONBytes += len(s)
				totalProbes++
				if totalJSONBytes > maxCorpusBytes || totalProbes > 200000 {
					budgetErr = fmt.Errorf("schema oracle %s: total corpus byte/probe bound exceeded (bytes=%d probes=%d)", name, totalJSONBytes, totalProbes)
					return
				}
				seen[s] = true
				target.Probes = append(target.Probes, Probe{s, compiled.Validate(v) == nil})
			}
		}
		valid := 0
		mutated := map[string]bool{}
		discriminators := unionDiscriminators(doc, root, 0)
		nestedDiscriminators := allDiscriminators(doc, root, map[string]bool{}, 0)
		for _, v := range values {
			if compiled.Validate(v) != nil {
				return nil, fmt.Errorf("schema oracle %s: cannot construct valid witness at root: %v", name, compiled.Validate(v))
			}
			valid++
			add(v)
			context := mutationShape(v, 0)
			if len(discriminators) > 0 {
				context = fmt.Sprintf("%T", v)
			}
			if object, ok := v.(map[string]any); ok {
				for _, key := range discriminators {
					value, _ := json.Marshal(object[key])
					context += "/" + strconv.Quote(key) + ":" + string(value)
				}
			}
			if target.CheckFields {
				context = "object"
			}
			mutations, err := mutations(v, context, mutated, nestedDiscriminators)
			if err != nil {
				return nil, fmt.Errorf("schema oracle %s: %w", name, err)
			}
			for _, mutation := range mutations {
				add(mutation)
			}
			if budgetErr != nil {
				return nil, budgetErr
			}
		}
		if valid == 0 {
			return nil, fmt.Errorf("schema oracle %s: no valid witness", name)
		}
		if len(target.Probes) > 40000 {
			return nil, fmt.Errorf("schema oracle %s: probe bound exceeded (%d)", name, len(target.Probes))
		}
		result = append(result, target)
	}
	return result, nil
}

// Generate renders a stdlib-only test. Candidate tests cannot select the corpus
// or decide which mutations are valid. Decode success also requires lossless
// JSON roundtrip, including optional absence, null, and distinct field values.
func Generate(req Request) ([]byte, error) {
	targets, err := Build(req)
	if err != nil {
		return nil, err
	}
	pkg := req.Package
	if pkg == "" {
		pkg = "protocolv2"
	}
	if !token.IsIdentifier(pkg) {
		return nil, fmt.Errorf("schema oracle: invalid package")
	}
	corpus := make([][]Probe, len(targets))
	for i, t := range targets {
		corpus[i] = t.Probes
	}
	raw, err := json.Marshal(corpus)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxCorpusBytes {
		return nil, fmt.Errorf("schema oracle: corpus exceeds %d decompressed bytes", maxCorpusBytes)
	}
	var compressed bytes.Buffer
	zip := gzip.NewWriter(&compressed)
	if _, err := zip.Write(raw); err != nil {
		return nil, err
	}
	if err := zip.Close(); err != nil {
		return nil, err
	}
	if compressed.Len() > maxCompressedBytes {
		return nil, fmt.Errorf("schema oracle: corpus exceeds %d compressed bytes", maxCompressedBytes)
	}
	var b strings.Builder
	typePrefix := ""
	fmt.Fprintf(&b, "package %s\nimport (\"encoding/json\";\"bytes\";\"reflect\";\"testing\";\"sort\";\"strings\";\"strconv\";\"compress/gzip\";\"encoding/base64\";\"io\";\"math/big\")\n", pkg)
	if req.ImportPath != "" {
		fmt.Fprintf(&b, "import subject %s\n", strconv.Quote(req.ImportPath))
		typePrefix = "subject."
	}
	fmt.Fprintf(&b, "const frozenCorpus = %s\nconst maxFrozenCorpusBytes = %d\nconst maxFrozenCompressedBytes = %d\nconst frozenTargetCount = %d\n", strconv.Quote(base64.StdEncoding.EncodeToString(compressed.Bytes())), maxCorpusBytes, maxCompressedBytes, len(targets))
	b.WriteString(`func TestTrustedSchemaOracle(t *testing.T) {
 type probe struct { JSON string; Valid bool }
 if len(frozenCorpus)>((maxFrozenCompressedBytes+2)/3)*4{t.Fatal("compressed corpus bound exceeded")}
 encoded,err:=base64.StdEncoding.DecodeString(frozenCorpus);if err!=nil{t.Fatal(err)};if len(encoded)>maxFrozenCompressedBytes{t.Fatal("compressed corpus bound exceeded")}
 reader,err:=gzip.NewReader(bytes.NewReader(encoded));if err!=nil{t.Fatal(err)}
 raw,err:=io.ReadAll(io.LimitReader(reader,maxFrozenCorpusBytes+1));closeErr:=reader.Close();if err!=nil{t.Fatal(err)};if closeErr!=nil{t.Fatal(closeErr)};if len(raw)>maxFrozenCorpusBytes{t.Fatal("decompressed corpus bound exceeded")}
 var corpus [][]probe;if err:=json.Unmarshal(raw,&corpus);err!=nil{t.Fatal(err)};if len(corpus)!=frozenTargetCount{t.Fatal("frozen target count mismatch")}
 preview:=func(data string)string{if len(data)>2048{return data[:2048]+"...[truncated]"};return data}
 var equal func(any,any)bool
 equal=func(a,b any)bool{switch x:=a.(type){case json.Number:y,ok:=b.(json.Number);if !ok{return false};r,ok:=new(big.Rat).SetString(string(x));if !ok{return false};s,ok:=new(big.Rat).SetString(string(y));return ok&&r.Cmp(s)==0;case map[string]any:y,ok:=b.(map[string]any);if !ok||len(x)!=len(y){return false};for k,v:=range x{other,exists:=y[k];if !exists||!equal(v,other){return false}};return true;case []any:y,ok:=b.([]any);if !ok||len(x)!=len(y){return false};for i,v:=range x{if !equal(v,y[i]){return false}};return true;default:return reflect.DeepEqual(a,b)}}
 check := func(t *testing.T, factory func() any, fields []string, checkFields bool, probes []probe) {
  if checkFields { typ:=reflect.TypeOf(factory()).Elem();if typ.Kind()!=reflect.Struct{t.Fatalf("schema object must bind struct, got %s",typ)};actual:=[]string{};for i:=0;i<typ.NumField();i++ {f:=typ.Field(i);if f.PkgPath!=""{continue};tag:=strings.Split(f.Tag.Get("json"),",")[0];if tag=="-"{continue};if tag==""{tag=f.Name};actual=append(actual,tag)};sort.Strings(actual);if !reflect.DeepEqual(actual,fields){t.Fatalf("schema fields=%v Go JSON fields=%v",fields,actual)} }

  for i, p := range probes { t.Run(strconv.Itoa(i), func(t *testing.T) {
   target := factory(); err := json.Unmarshal([]byte(p.JSON),target)
   if !p.Valid { if err == nil { t.Fatalf("schema-invalid input accepted: %s",preview(p.JSON)) }; return }
   if err != nil { t.Fatalf("schema-valid input rejected: %s: %v",preview(p.JSON),err) }
   actual,err:=json.Marshal(target);if err!=nil{t.Fatal(err)}
   decode:=func(raw []byte) any { var v any; d:=json.NewDecoder(bytes.NewReader(raw));d.UseNumber();if err:=d.Decode(&v);err!=nil{t.Fatal(err)};return v }
   if !equal(decode([]byte(p.JSON)),decode(actual)){t.Fatalf("wire value lost: input=%s output=%s",preview(p.JSON),preview(string(actual)))}
  }) }
 }
`)
	for i, t := range targets {
		fmt.Fprintf(&b, "t.Run(%s,func(t *testing.T){check(t,func() any{return new(%s)},[]string{", strconv.Quote(t.Schema), typePrefix+t.GoType)
		for _, field := range t.Fields {
			fmt.Fprintf(&b, "%s,", strconv.Quote(field))
		}
		fmt.Fprintf(&b, "},%t,corpus[%d])})\n", t.CheckFields, i)
	}
	b.WriteString("}\n")
	if b.Len() > 12<<20 {
		return nil, fmt.Errorf("schema oracle: generated source exceeds 12 MiB")
	}
	return format.Source([]byte(b.String()))
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// prepare audits schema keywords (not instance property names), prohibits all
// network/relative references and IDs, and applies the stated local overlay.
func prepare(n any, path string, close bool, depth int) error {
	if depth > 100 {
		return fmt.Errorf("%s: schema depth bound exceeded", path)
	}
	if _, ok := n.(bool); ok {
		return nil
	}
	m, ok := n.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: expected schema object", path)
	}
	if err := flattenObjectAlternatives(m, path); err != nil {
		return err
	}
	for _, k := range keys(m) {
		v := m[k]
		switch k {
		case "$schema":
			if v != "http://json-schema.org/draft-07/schema#" && v != "https://json-schema.org/draft/2020-12/schema" {
				return fmt.Errorf("%s: unsupported dialect %v", path, v)
			}
		case "$ref":
			r, ok := v.(string)
			if !ok || !strings.HasPrefix(r, "#/") {
				return fmt.Errorf("%s: only local JSON-pointer references supported", path)
			}
		case "title", "description", "default", "examples", "deprecated", "readOnly", "writeOnly", "type", "enum", "const", "required", "format", "minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems":
		case "definitions", "$defs": // Only referenced definitions are part of this target.
		case "properties":
			children, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("%s/%s: expected object", path, k)
			}
			for _, name := range keys(children) {
				if err := prepare(children[name], path+"/"+k+"/"+name, close, depth+1); err != nil {
					return err
				}
			}
		case "items", "additionalProperties":
			if err := prepare(v, path+"/"+k, close, depth+1); err != nil {
				return err
			}
		case "anyOf", "oneOf", "allOf":
			children, ok := v.([]any)
			if !ok || len(children) == 0 {
				return fmt.Errorf("%s/%s: empty or invalid alternatives", path, k)
			}
			for i, c := range children {
				if err := prepare(c, fmt.Sprintf("%s/%s/%d", path, k, i), close, depth+1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("%s: unsupported verifier keyword %s", path, k)
		}
	}
	if close {
		if _, has := m["properties"]; has {
			if _, explicit := m["additionalProperties"]; !explicit {
				m["additionalProperties"] = false
			}
		}
	}
	return nil
}

func objectFields(doc, node any, depth int) ([]string, bool, error) {
	if depth > 30 {
		return nil, false, fmt.Errorf("object binding reference depth exceeded")
	}
	m, ok := node.(map[string]any)
	if !ok {
		return nil, false, nil
	}
	if r, ok := m["$ref"].(string); ok {
		n, e := pointer(doc, r)
		if e != nil {
			return nil, false, e
		}
		return objectFields(doc, n, depth+1)
	}
	if a, ok := m["allOf"].([]any); ok && len(a) == 1 {
		return objectFields(doc, a[0], depth+1)
	}
	p, ok := m["properties"].(map[string]any)
	if !ok {
		return nil, false, nil
	}
	return keys(p), true, nil
}

func prepareReachable(doc, node any, path string, close bool, seen map[string]bool) error {
	if err := prepare(node, path, close, 0); err != nil {
		return err
	}
	var visit func(any) error
	visit = func(n any) error {
		m, ok := n.(map[string]any)
		if !ok {
			return nil
		}
		if ref, ok := m["$ref"].(string); ok && !seen[ref] {
			seen[ref] = true
			target, e := pointer(doc, ref)
			if e != nil {
				return e
			}
			if e := prepareReachable(doc, target, ref, close, seen); e != nil {
				return e
			}
		}
		for _, k := range []string{"properties"} {
			if props, ok := m[k].(map[string]any); ok {
				for _, name := range keys(props) {
					if e := visit(props[name]); e != nil {
						return e
					}
				}
			}
		}
		for _, k := range []string{"items", "additionalProperties"} {
			if e := visit(m[k]); e != nil {
				return e
			}
		}
		for _, k := range []string{"anyOf", "oneOf", "allOf"} {
			if a, ok := m[k].([]any); ok {
				for _, n := range a {
					if e := visit(n); e != nil {
						return e
					}
				}
			}
		}
		return nil
	}
	return visit(node)
}

// JSON Schema combines sibling properties with anyOf conjunctively. Normalize
// the small object-product form before adding the strict-object overlay, so
// closure applies to the complete alternative, never half an object.
func flattenObjectAlternatives(m map[string]any, path string) error {
	props, ok := m["properties"].(map[string]any)
	if !ok {
		return nil
	}
	for _, kind := range []string{"anyOf", "oneOf"} {
		alts, ok := m[kind].([]any)
		if !ok {
			continue
		}
		if _, ok := m["additionalProperties"]; ok {
			return fmt.Errorf("%s: explicit closure with sibling %s unsupported", path, kind)
		}
		merged := make([]any, 0, len(alts))
		for _, a := range alts {
			branch, ok := a.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: nonobject compound alternative", path)
			}
			bp, ok := branch["properties"].(map[string]any)
			if !ok {
				return fmt.Errorf("%s: compound alternative lacks properties", path)
			}
			if _, ok := branch["additionalProperties"]; ok {
				return fmt.Errorf("%s: explicit branch closure with sibling properties unsupported", path)
			}
			copy := cloneMap(m)
			delete(copy, "definitions")
			delete(copy, "$defs")
			delete(copy, "$schema")
			delete(copy, kind)
			p := cloneMap(props)
			for k, v := range bp {
				if _, exists := p[k]; exists {
					return fmt.Errorf("%s: overlapping compound property %s unsupported", path, k)
				}
				p[k] = v
			}
			copy["properties"] = p
			req, _ := m["required"].([]any)
			br, _ := branch["required"].([]any)
			copy["required"] = append(append([]any{}, req...), br...)
			for k, v := range branch {
				if k == "properties" || k == "required" || k == "title" || k == "description" {
					continue
				}
				if prev, exists := copy[k]; exists {
					a, _ := json.Marshal(prev)
					b, _ := json.Marshal(v)
					if !bytes.Equal(a, b) {
						return fmt.Errorf("%s: conflicting compound keyword %s", path, k)
					}
				}
				copy[k] = v
			}
			merged = append(merged, copy)
		}
		retained := map[string]any{}
		for _, key := range []string{"definitions", "$defs", "$schema", "title", "description"} {
			if value, ok := m[key]; ok {
				retained[key] = value
			}
		}
		for k := range m {
			delete(m, k)
		}
		for k, value := range retained {
			m[k] = value
		}
		m[kind] = merged
		return nil
	}
	return nil
}
func mutationShape(v any, depth int) string {
	switch x := v.(type) {
	case map[string]any:
		var b strings.Builder
		b.WriteString("object:")
		for _, k := range keys(x) {
			b.WriteString(strconv.Quote(k))
			if depth > 0 {
				b.WriteString(mutationShape(x[k], depth-1))
			}
		}
		return b.String()
	case []any:
		if depth > 0 && len(x) > 0 {
			return "array:" + mutationShape(x[0], depth-1)
		}
		return "array"
	default:
		return fmt.Sprintf("%T", v)
	}
}

type denyLoader struct{}

func (denyLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("schema oracle: external resource loading forbidden: %s", url)
}

// Preserve branch-specific negative obligations even when alternatives have the
// same property names. Discriminator keys come from schema const/enum facts.
func unionDiscriminators(doc, node any, depth int) []string {
	if depth > 30 {
		return nil
	}
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	if ref, ok := m["$ref"].(string); ok {
		n, e := pointer(doc, ref)
		if e != nil {
			return nil
		}
		return unionDiscriminators(doc, n, depth+1)
	}
	found := map[string]bool{}
	for _, kind := range []string{"anyOf", "oneOf"} {
		alternatives, ok := m[kind].([]any)
		if !ok {
			continue
		}
		constraints := map[string]map[string]bool{}
		occurrences := map[string]int{}
		objects := 0
		for _, alternative := range alternatives {
			branch, ok := alternative.(map[string]any)
			if !ok {
				continue
			}
			if ref, ok := branch["$ref"].(string); ok {
				if n, e := pointer(doc, ref); e == nil {
					branch, _ = n.(map[string]any)
				}
			}
			props, ok := branch["properties"].(map[string]any)
			if !ok {
				continue
			}
			objects++
			for key, node := range props {
				property, ok := node.(map[string]any)
				if !ok {
					continue
				}
				if ref, ok := property["$ref"].(string); ok {
					if n, e := pointer(doc, ref); e == nil {
						property, _ = n.(map[string]any)
					}
				}
				value, has := property["const"]
				if !has {
					value, has = property["enum"]
				}
				if !has {
					continue
				}
				raw, _ := json.Marshal(value)
				if constraints[key] == nil {
					constraints[key] = map[string]bool{}
				}
				constraints[key][string(raw)] = true
				occurrences[key]++
			}
		}
		for key, values := range constraints {
			if objects > 1 && occurrences[key] == objects && len(values) > 1 {
				found[key] = true
			}
		}
	}
	return keys(found)
}

func allDiscriminators(doc, node any, seen map[string]bool, depth int) []string {
	if depth > 100 {
		return nil
	}
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	found := map[string]bool{}
	for _, key := range unionDiscriminators(doc, node, 0) {
		found[key] = true
	}
	visit := func(node any) {
		for _, key := range allDiscriminators(doc, node, seen, depth+1) {
			found[key] = true
		}
	}
	if ref, ok := m["$ref"].(string); ok && !seen[ref] {
		seen[ref] = true
		if n, e := pointer(doc, ref); e == nil {
			visit(n)
		}
	}
	if properties, ok := m["properties"].(map[string]any); ok {
		for _, key := range keys(properties) {
			visit(properties[key])
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		visit(m[key])
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		if children, ok := m[key].([]any); ok {
			for _, child := range children {
				visit(child)
			}
		}
	}
	return keys(found)
}

// Package schemaoracle derives standalone wire obligations from source schemas.
// It never imports the generator or candidate runtime types.
package schemaoracle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/token"
	"io"
	"sort"
	"strconv"
	"strings"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// Request binds independently selected exact schema documents to Go identifiers.
// Bindings select independently determined reachable targets, including optional
// #/definitions/... fragments. Schemas includes their full owning documents.
// Candidate declarations may supply names only, never select this target set.
// CloseObjects is the standalone public json.Unmarshal overlay: property-bearing objects
// and named object records (including empty records) reject unknown fields unless
// explicitly open. Unconstrained JSON and anonymous union catch-alls stay open.
type Request struct {
	// Context carries the existing repair deadline through bounded construction.
	Context      context.Context
	Schemas      map[string][]byte
	Bindings     map[string]string
	ImportPath   string
	CloseObjects bool
}

type Probe struct {
	JSON       string
	Valid      bool
	Diagnostic string              `json:",omitempty"`
	Location   *DiagnosticLocation `json:",omitempty"`
}
type Target struct {
	Schema, GoType string
	Fields         []string
	CheckFields    bool
	// Only the source-selected ClientRequest envelope may canonically omit
	// optional null-only params. Other object members retain exact presence.
	ParameterlessMethods []string
}

// Walk derives and consumes obligations one at a time. A nil probe starts a target.
// Bounds reject incomplete work; they never sample or truncate coverage.
func Walk(req Request, yield func(Target, *Probe) error) error {
	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(req.Bindings) == 0 {
		return fmt.Errorf("schema oracle: no target schemas")
	}
	if len(req.Bindings) > 2048 {
		return fmt.Errorf("schema oracle: target count exceeds 2048")
	}
	totalProbes := 0
	var totalBytes int64
	var budgetErr error
	for _, name := range keys(req.Bindings) {
		if err := ctx.Err(); err != nil {
			return err
		}
		binding := req.Bindings[name]
		if !token.IsIdentifier(binding) || token.Lookup(binding).IsKeyword() {
			return fmt.Errorf("schema oracle %s: missing or invalid Go binding %q", name, binding)
		}
		docName, fragment, _ := strings.Cut(name, "#")
		fragment = "#" + fragment
		raw, exists := req.Schemas[docName]
		if !exists {
			return fmt.Errorf("schema oracle %s: missing schema document", name)
		}
		if len(raw) > 16<<20 {
			return fmt.Errorf("schema oracle %s: document exceeds 16 MiB", name)
		}
		var doc any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			return fmt.Errorf("schema oracle %s: %w", name, err)
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("schema oracle %s: trailing JSON data", name)
		}
		var original any
		originalDecoder := json.NewDecoder(bytes.NewReader(raw))
		originalDecoder.UseNumber()
		if err := originalDecoder.Decode(&original); err != nil {
			return err
		}
		root, err := pointer(doc, fragment)
		if err != nil {
			return fmt.Errorf("schema oracle %s: %w", name, err)
		}
		parameterlessMethods := parameterlessRequestMethods(root, docName, fragment)
		if err := prepareReachable(doc, root, fragment, req.CloseObjects, map[string]bool{}); err != nil {
			return fmt.Errorf("schema oracle %s: %w", name, err)
		}
		c := validator.NewCompiler()
		c.UseLoader(denyLoader{})
		const url = "https://schema-oracle.invalid/target.json"
		if err := c.AddResource(url, doc); err != nil {
			return fmt.Errorf("schema oracle %s: %w", name, err)
		}
		compiled, err := c.Compile(url + fragment)
		if err != nil {
			return fmt.Errorf("schema oracle %s: %w", name, err)
		}
		sourceSchema := compiled
		hasOutputSchema, err := applyOutputSchemaOverlay(doc, root, fragment, docName)
		if err != nil {
			return fmt.Errorf("schema oracle %s: %w", name, err)
		}
		contractDoc, hasSDKOverlay, err := ContractDocument(doc, fragment, docName)
		if err != nil {
			return fmt.Errorf("schema oracle %s: %w", name, err)
		}
		if hasOutputSchema || hasSDKOverlay {
			c = validator.NewCompiler()
			c.UseLoader(denyLoader{})
			c.RegisterVocabulary(outputSchemaVocabulary())
			c.AssertVocabs()
			if err := c.AddResource(url, contractDoc); err != nil {
				return err
			}
			compiled, err = c.Compile(url + fragment)
			if err != nil {
				return fmt.Errorf("schema oracle %s: %w", name, err)
			}
		}
		w := witnesses{doc: doc}
		values, err := w.make(root, fragment, 0)
		if err != nil {
			return fmt.Errorf("schema oracle %s: %w", name, err)
		}
		target := Target{Schema: name, GoType: binding}
		target.ParameterlessMethods = parameterlessMethods
		target.Fields, target.CheckFields, err = objectFields(doc, root, 0)
		if err != nil {
			return fmt.Errorf("schema oracle %s: %w", name, err)
		}
		if err := yield(target, nil); err != nil {
			return err
		}
		seen := map[[32]byte]bool{}
		probeCount := 0
		add := func(v any, mutationPath string) {
			if err := ctx.Err(); err != nil {
				budgetErr = err
				return
			}
			if budgetErr != nil {
				return
			}
			b, _ := json.Marshal(v)
			if len(b) > maxValueBytes {
				budgetErr = fmt.Errorf("schema oracle %s: value framing bound exceeded", name)
				return
			}
			hash := sha256.Sum256(b)
			if !seen[hash] {
				seen[hash] = true
				totalProbes++
				probeCount++
				totalBytes += int64(len(b))
				// Total serialized work, not an RSS limit. Retained witnesses and
				// mutation traversal have their own recursion/work bounds.
				if totalProbes > maxProbes || probeCount > maxTargetProbes || totalBytes > maxWorkBytes {
					budgetErr = fmt.Errorf("schema oracle %s: total work bound exceeded", name)
					return
				}
				validationErr := compiled.Validate(v)
				p := Probe{JSON: string(b), Valid: validationErr == nil}
				if validationErr != nil {
					p.Location = &DiagnosticLocation{}
					p.Diagnostic = invalidDiagnostic(original, docName, v, mutationPath, validationErr, p.Location)
				}
				budgetErr = yield(target, &p)
			}
		}
		valid := 0
		mutated := map[string]bool{}
		discriminators := unionDiscriminators(doc, root, 0)
		nestedDiscriminators := allDiscriminators(doc, root, map[string]bool{}, 0)
		for _, v := range values {
			if err := ctx.Err(); err != nil {
				return err
			}
			if sourceSchema.Validate(v) != nil {
				return fmt.Errorf("schema oracle %s: cannot construct valid witness at root: %v", name, sourceSchema.Validate(v))
			}
			if compiled.Validate(v) == nil {
				valid++
			}
			add(v, "#")
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
			if target.CheckFields || len(target.Fields) > 0 {
				context = "object"
			}
			mutations, err := mutations(v, context, mutated, nestedDiscriminators)
			if err != nil {
				return fmt.Errorf("schema oracle %s: %w", name, err)
			}
			for _, mutation := range mutations {
				add(mutation.Value, mutation.Path)
			}
			if budgetErr != nil {
				return budgetErr
			}
		}
		if valid == 0 {
			return fmt.Errorf("schema oracle %s: no valid witness", name)
		}
	}
	return nil
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
	return prepareNode(n, path, close, true, depth)
}

func prepareNode(n any, path string, close, closeSelf bool, depth int) error {
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
	if err := flattenObjectAlternatives(m, path, close); err != nil {
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
				if err := prepareNode(c, fmt.Sprintf("%s/%s/%d", path, k, i), close, k != "anyOf", depth+1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("%s: unsupported verifier keyword %s", path, k)
		}
	}
	if close && closeSelf {
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
	// Open records may carry dynamic properties in a custom marshaler. Their
	// actual wire fields are checked by classified round trips, not struct layout.
	if extra, exists := m["additionalProperties"]; exists {
		if closed, isBool := extra.(bool); !isBool || closed {
			if len(p) > 0 {
				return keys(p), false, nil
			}
			return nil, false, nil
		}
	}

	return keys(p), true, nil
}

func prepareReachable(doc, node any, path string, close bool, seen map[string]bool) error {
	if close && namedRecordPosition(path) {
		if m, ok := node.(map[string]any); ok && emptyObjectRecord(m) {
			// Named empty records have the same field-set contract as records
			// with declared properties. Reuse the existing overlay and checker.
			m["properties"] = map[string]any{}
		}
	}
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
func flattenObjectAlternatives(m map[string]any, path string, close bool) error {
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
		// Closure belongs to the complete record. Other alternatives' fields
		// remain unconstrained in an open anyOf branch, even when present.
		unionProperties := cloneMap(props)
		if kind == "anyOf" {
			for _, a := range alts {
				if branch, ok := a.(map[string]any); ok {
					if bp, ok := branch["properties"].(map[string]any); ok {
						for name := range bp {
							if _, exists := unionProperties[name]; !exists {
								unionProperties[name] = true
							}
						}
					}
				}
			}
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
			p := cloneMap(unionProperties)
			for k, v := range bp {
				if _, exists := props[k]; exists {
					return fmt.Errorf("%s: overlapping compound property %s unsupported", path, k)
				}
				p[k] = v
			}
			copy["properties"] = p
			if close {
				copy["additionalProperties"] = false
			}
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

// Titles are annotations, not naming authority. Only a document root or a
// direct definition is a named binding; a ref into an anonymous union branch
// does not promote that branch into a closed record.
func namedRecordPosition(path string) bool {
	if path == "#" {
		return true
	}
	for _, prefix := range []string{"#/definitions/", "#/$defs/"} {
		if name, ok := strings.CutPrefix(path, prefix); ok && name != "" && !strings.Contains(name, "/") {
			return true
		}
	}
	return false
}

func emptyObjectRecord(m map[string]any) bool {
	kind, _ := m["type"].(string)
	object := kind == "object"
	if types, ok := m["type"].([]any); ok {
		object = false
		if len(types) == 1 {
			kind, _ := types[0].(string)
			object = kind == "object"
		}
	}
	if !object {
		return false
	}
	for _, key := range []string{"properties", "$ref", "anyOf", "oneOf", "allOf"} {
		if _, exists := m[key]; exists {
			return false
		}
	}
	if extra, exists := m["additionalProperties"]; exists {
		closed, ok := extra.(bool)
		return ok && !closed
	}
	return true
}

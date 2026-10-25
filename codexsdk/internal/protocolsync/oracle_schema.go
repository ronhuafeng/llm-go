package protocolsync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolupgrade"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

type oracleSchemaFact struct {
	key, shape string
	refs       map[string]bool
	inline     bool
}

// schemaOraclePublicRequest scopes expectations to upstream public entrypoints.
// Candidate generated declarations never decide reachability.
func schemaOraclePublicRequest(baseline, candidate, generated, commonRS string) (schemaoracle.Request, error) {
	roots, err := protocolupgrade.OraclePublicRoots(candidate, commonRS)
	if err != nil {
		return schemaoracle.Request{}, &oracleContractError{err}
	}
	oldRoots, err := protocolupgrade.OracleAcceptedPublicRoots(baseline)
	if err != nil {
		return schemaoracle.Request{}, &oracleContractError{err}
	}
	return schemaOracleScopedRequest(baseline, candidate, generated, oldRoots, roots)
}

// schemaOracleRequest is the unscoped current-graph seam for focused tests.
func schemaOracleRequest(baseline, candidate, generated string) (schemaoracle.Request, error) {
	return schemaOracleScopedRequest(baseline, candidate, generated, nil, nil)
}

func schemaOracleScopedRequest(baseline, candidate, generated string, oldRoots, roots map[string]bool) (schemaoracle.Request, error) {
	old, _, err := oracleSchemaFacts(baseline)
	if err != nil {
		return schemaoracle.Request{}, err
	}
	next, documents, err := oracleSchemaFacts(candidate)
	if err != nil {
		return schemaoracle.Request{}, err
	}
	if roots != nil {
		if err := oracleScopeFacts(old, oldRoots); err != nil {
			return schemaoracle.Request{}, &oracleContractError{err}
		}
		if err := oracleScopeFacts(next, roots); err != nil {
			return schemaoracle.Request{}, &oracleContractError{err}
		}
	}

	// A generated child decoder cannot certify its handwritten transport
	// consumer. Guard the entire accepted reference closure, including removal.
	for name, prior := range old {
		if !oracleEnvelopeName(name) {
			continue
		}
		if dependency := changedTransportDependency(old, next, name, map[string]bool{}); dependency != "" {
			return schemaoracle.Request{}, &oracleContractError{fmt.Errorf("schema oracle: changed handwritten transport contract %s (dependency %s) needs an explicit probe binding", prior.key, dependency)}
		}
	}

	// Old facts protect removed public surface and handwritten envelopes only.
	// Current reachability, including unchanged schemas, selects runtime targets.
	for name, prior := range old {
		if _, exists := next[name]; !exists && !prior.inline && !oracleEnvelopeName(name) {
			found, e := oracleDeclaredType(generated, name)
			if e != nil {
				return schemaoracle.Request{}, &oracleContractError{e}
			}
			if found {
				return schemaoracle.Request{}, fmt.Errorf("schema oracle: removed source type %s remains generated", name)
			}
		}
	}
	bindings := map[string]string{}
	for name, fact := range next {
		if fact.inline {
			continue
		} // Verified through selected owners below.

		if !token.IsIdentifier(name) || token.Lookup(name).IsKeyword() || !ast.IsExported(name) {
			return schemaoracle.Request{}, &oracleContractError{fmt.Errorf("schema oracle: source %s has unsupported Go binding %q", fact.key, name)}
		}
		if oracleEnvelopeName(name) {
			if _, exists := old[name]; !exists {
				return schemaoracle.Request{}, &oracleContractError{fmt.Errorf("schema oracle: new handwritten transport contract %s needs an explicit probe binding", fact.key)}
			}
			continue // Closure guard above plus immutable accepted regressions.
		}
		bindings[fact.key] = name
	}
	// Inline aliases must be exercised through a real selected owner.
	for name, fact := range next {
		if !fact.inline {
			continue
		}
		covered := false
		for owner, current := range next {
			if !current.inline && !oracleEnvelopeName(owner) && oracleFactReferences(next, owner, name, map[string]bool{}) {
				covered = true
				break
			}
		}
		if !covered {
			return schemaoracle.Request{}, &oracleContractError{fmt.Errorf("schema oracle: inline source %s has no independently selected probe owner", fact.key)}
		}
	}
	return schemaoracle.Request{Schemas: documents, Bindings: bindings, ImportPath: "github.com/ronhuafeng/llm-go/codexsdk/protocolv2", CloseObjects: true}, nil
}

func oracleSchemaFacts(root string) (facts map[string]oracleSchemaFact, documents map[string][]byte, err error) {
	defer func() {
		if err != nil {
			err = &oracleContractError{err}
		}
	}()
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		// These exact files are projections or duplicate aggregate containers, not
		// independently generated payload roots. Every other raw schema is selected,
		// including root-level requests/responses, v1, v2, and future directories.
		switch rel {
		case "baseline_metadata.json", "coverage_matrix.json", "drift_report.json", "manifest.json", "manifest_generation.json", "matrix_update_skeleton.json", "codex_app_server_protocol.schemas.json", "codex_app_server_protocol.v2.schemas.json":
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("schema oracle: source symlink %s is unsupported", rel)
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, nil, fmt.Errorf("schema oracle: no raw source schemas in %s", root)
	}
	facts = map[string]oracleSchemaFact{}
	documents = map[string][]byte{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var doc map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err = decoder.Decode(&doc); err != nil {
			return nil, nil, err
		}
		if e := decoder.Decode(new(any)); e != io.EOF {
			return nil, nil, fmt.Errorf("schema oracle: trailing JSON data in %s", path)
		}
		if doc == nil {
			return nil, nil, fmt.Errorf("schema oracle: source %s is not an object schema", path)
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return nil, nil, e
		}
		key := filepath.ToSlash(rel)
		if err := oracleSourceStructure(doc, key, true); err != nil {
			return nil, nil, err
		}
		documents[key] = raw
		typed, _, err := schemaoracle.ContractDocument(doc, "#", key)
		if err != nil {
			return nil, nil, err
		}
		name, _ := doc["title"].(string)
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), ".json")
		}
		nodes := map[string]any{name: doc}
		pointers := map[string]string{name: key}
		for _, defsKey := range []string{"definitions", "$defs"} {
			value, present := doc[defsKey]
			if !present {
				continue
			}
			defs, ok := value.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("schema oracle: %s/%s is not a definition map", key, defsKey)
			}
			for n, node := range defs {
				switch node.(type) {
				case map[string]any, bool:
				default:
					return nil, nil, fmt.Errorf("schema oracle: %s/%s/%s is not a schema", key, defsKey, n)
				}
				pointer := key + "#/" + defsKey + "/" + strings.ReplaceAll(strings.ReplaceAll(n, "~", "~0"), "/", "~1")
				if previous, exists := nodes[n]; exists {
					a, _ := json.Marshal(oracleShape(previous))
					b, _ := json.Marshal(oracleShape(node))
					if !bytes.Equal(a, b) {
						return nil, nil, fmt.Errorf("schema oracle: ambiguous source type %s in %s and %s", n, pointers[n], pointer)
					}
					continue
				}
				nodes[n] = node
				pointers[n] = pointer
			}
		}
		names := make([]string, 0, len(nodes))
		for n := range nodes {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			node := nodes[n]
			pointer := pointers[n]
			shape, err := json.Marshal(struct {
				Dialect any
				Shape   any
			}{doc["$schema"], oracleShape(node)})
			if err != nil {
				return nil, nil, err
			}
			refs := map[string]bool{}
			typedNode, err := oracleNode(typed, strings.TrimPrefix(pointer, key))
			if err != nil {
				return nil, nil, err
			}
			oracleRefs(typedNode, refs)
			schema, _ := node.(map[string]any)
			kind, _ := schema["type"].(string)
			inline := kind != "" && kind != "object" && kind != "array" && schema["enum"] == nil && schema["oneOf"] == nil && schema["anyOf"] == nil && schema["allOf"] == nil
			fact := oracleSchemaFact{key: pointer, shape: string(shape), refs: refs, inline: inline}
			if prior, ok := facts[n]; ok {
				if prior.shape != fact.shape {
					return nil, nil, fmt.Errorf("schema oracle: ambiguous source type %s in %s and %s", n, prior.key, pointer)
				}
				if prior.key < fact.key {
					continue
				}
			}
			facts[n] = fact
		}
	}
	return facts, documents, nil
}

// Shape comparison traverses schema positions only. Property names and literal
// default/enum/const JSON are data, even when named title or definitions.
func oracleShape(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := map[string]any{}
	for k, x := range m {
		switch k {
		case "definitions", "$defs", "description", "$schema", "$comment", "title":
			continue
		case "properties", "patternProperties", "dependentSchemas":
			if values, ok := x.(map[string]any); ok {
				children := map[string]any{}
				for name, node := range values {
					children[name] = oracleShape(node)
				}
				out[k] = children
			} else {
				out[k] = x
			}
		case "anyOf", "oneOf", "allOf", "prefixItems":
			if values, ok := x.([]any); ok {
				children := make([]any, len(values))
				for i, node := range values {
					children[i] = oracleShape(node)
				}
				out[k] = children
			} else {
				out[k] = x
			}
		case "items":
			if values, ok := x.([]any); ok {
				children := make([]any, len(values))
				for i, node := range values {
					children[i] = oracleShape(node)
				}
				out[k] = children
			} else {
				out[k] = oracleShape(x)
			}
		case "additionalProperties", "additionalItems", "contains", "not", "if", "then", "else", "propertyNames", "unevaluatedProperties", "unevaluatedItems":
			out[k] = oracleShape(x)
		default:
			out[k] = x
		}
	}
	return out
}
func oracleRefs(v any, refs map[string]bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	if s, ok := m["$ref"].(string); ok {
		if strings.HasPrefix(s, "#/definitions/") || strings.HasPrefix(s, "#/$defs/") {
			owner := strings.Split(strings.TrimPrefix(strings.TrimPrefix(s, "#/definitions/"), "#/$defs/"), "/")[0]
			refs[strings.ReplaceAll(strings.ReplaceAll(owner, "~1", "/"), "~0", "~")] = true
		} else {
			refs[strings.TrimSuffix(filepath.Base(strings.Split(s, "#")[0]), ".json")] = true
		}
	}
	for k, x := range m {
		switch k {
		case "properties", "patternProperties", "dependentSchemas":
			if values, ok := x.(map[string]any); ok {
				for _, node := range values {
					oracleRefs(node, refs)
				}
			}
		case "anyOf", "oneOf", "allOf", "prefixItems":
			if values, ok := x.([]any); ok {
				for _, node := range values {
					oracleRefs(node, refs)
				}
			}
		case "items":
			if values, ok := x.([]any); ok {
				for _, node := range values {
					oracleRefs(node, refs)
				}
			} else {
				oracleRefs(x, refs)
			}
		case "additionalProperties", "additionalItems", "contains", "not", "if", "then", "else", "propertyNames", "unevaluatedProperties", "unevaluatedItems":
			oracleRefs(x, refs)
		}
	}
}

// Only handwritten envelope exemptions need dependency drift propagation.
func changedTransportDependency(old, next map[string]oracleSchemaFact, name string, seen map[string]bool) string {
	if seen[name] {
		return ""
	}
	seen[name] = true
	prior, existed := old[name]
	current, exists := next[name]
	if !existed || !exists || prior.shape != current.shape {
		return name
	}
	for ref := range prior.refs {
		if changed := changedTransportDependency(old, next, ref, seen); changed != "" {
			return changed
		}
	}
	return ""
}

func oracleEnvelopeName(name string) bool {
	switch name {
	case "JSONRPCRequest", "JSONRPCResponse", "JSONRPCNotification", "JSONRPCMessage", "JSONRPCError", "JSONRPCErrorError":
		return true
	}
	return false
}
func oracleDeclaredType(dir, name string) (bool, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("schema oracle: generated source path is not a directory: %s", dir)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.gen.go"))
	if err != nil {
		return false, err
	}
	if len(paths) == 0 {
		return false, fmt.Errorf("schema oracle: no generated declarations available in %s", dir)
	}
	for _, p := range paths {
		f, e := parser.ParseFile(token.NewFileSet(), p, nil, 0)
		if e != nil {
			return false, e
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, s := range g.Specs {
				if t, ok := s.(*ast.TypeSpec); ok && t.Name.Name == name {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func oracleFactReferences(facts map[string]oracleSchemaFact, owner, target string, seen map[string]bool) bool {
	if seen[owner] {
		return false
	}
	seen[owner] = true
	fact, ok := facts[owner]
	if !ok {
		return false
	}
	if fact.refs[target] {
		return true
	}
	for ref := range fact.refs {
		if oracleFactReferences(facts, ref, target, seen) {
			return true
		}
	}
	return false
}

// The binding map owns document roots and their top-level named definitions.
// Nested containers require an explicit binding extension, never silent omission.
func oracleSourceStructure(node any, path string, document bool) error {
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	if ref, ok := m["$ref"].(string); ok && !strings.HasPrefix(ref, "#") {
		return fmt.Errorf("schema oracle: external reference unsupported at %s: %s", path, ref)
	}
	for k, v := range m {
		switch k {
		case "definitions", "$defs":
			if !document {
				return fmt.Errorf("schema oracle: nested definition container requires binding support at %s/%s", path, k)
			}
			if children, ok := v.(map[string]any); ok {
				for name, child := range children {
					if err := oracleSourceStructure(child, path+"/"+k+"/"+name, false); err != nil {
						return err
					}
				}
			}
		case "properties", "patternProperties", "dependentSchemas":
			if children, ok := v.(map[string]any); ok {
				for name, child := range children {
					if err := oracleSourceStructure(child, path+"/"+k+"/"+name, false); err != nil {
						return err
					}
				}
			}
		case "anyOf", "oneOf", "allOf", "prefixItems":
			if children, ok := v.([]any); ok {
				for i, child := range children {
					if err := oracleSourceStructure(child, fmt.Sprintf("%s/%s/%d", path, k, i), false); err != nil {
						return err
					}
				}
			}
		case "items", "additionalProperties", "additionalItems", "contains", "not", "if", "then", "else", "propertyNames", "unevaluatedProperties", "unevaluatedItems":
			if children, ok := v.([]any); ok {
				for i, child := range children {
					if err := oracleSourceStructure(child, fmt.Sprintf("%s/%s/%d", path, k, i), false); err != nil {
						return err
					}
				}
			} else if err := oracleSourceStructure(v, path+"/"+k, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func oracleScopeFacts(facts map[string]oracleSchemaFact, roots map[string]bool) error {
	for name := range facts {
		if oracleEnvelopeName(name) || name == "RequestId" {
			roots[name] = true
		}
	}
	reachable := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if reachable[name] {
			return nil
		}
		fact, ok := facts[name]
		if !ok {
			return fmt.Errorf("schema oracle: public source root/reference %s has no schema binding", name)
		}
		reachable[name] = true
		for ref := range fact.refs {
			if err := visit(ref); err != nil {
				return err
			}
		}
		return nil
	}
	for name := range roots {
		if err := visit(name); err != nil {
			return err
		}
	}
	for name := range facts {
		if !reachable[name] {
			delete(facts, name)
		}
	}
	return nil
}

func oracleNode(doc any, fragment string) (any, error) {
	if fragment == "" || fragment == "#" {
		return doc, nil
	}
	node := doc
	for _, part := range strings.Split(strings.TrimPrefix(fragment, "#/"), "/") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid schema pointer %s", fragment)
		}
		var exists bool
		node, exists = m[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
		if !exists {
			return nil, fmt.Errorf("missing schema pointer %s", fragment)
		}
	}
	return node, nil
}

package schemaoracle

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/big"
	"strings"
)

type witnesses struct {
	doc    any
	count  int
	active map[string]bool
}

func pointer(doc any, ref string) (any, error) {
	if ref == "#" || ref == "" {
		return doc, nil
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("unsupported reference %q", ref)
	}
	value := doc
	for _, part := range strings.Split(ref[2:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		m, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("reference %s has non-object parent", ref)
		}
		value, ok = m[part]
		if !ok {
			return nil, fmt.Errorf("reference %s is missing", ref)
		}
	}
	return value, nil
}
func (w *witnesses) make(node any, path string, depth int) ([]any, error) {
	w.count++
	if depth > 30 || w.count > 10000 {
		return nil, fmt.Errorf("%s: witness recursion/size bound exceeded", path)
	}
	if b, ok := node.(bool); ok {
		if !b {
			return nil, fmt.Errorf("%s: false schema has no witness", path)
		}
		return freeValues(fmt.Sprint(w.count)), nil
	}
	m := node.(map[string]any)
	if r, ok := m["$ref"].(string); ok {
		n, err := pointer(w.doc, r)
		if err != nil {
			return nil, err
		}
		if w.active == nil {
			w.active = map[string]bool{}
		}
		if w.active[r] {
			v, e := w.terminal(n, path+"->"+r, map[string]bool{}, 0)
			if e != nil {
				return nil, e
			}
			return []any{v}, nil
		}
		w.active[r] = true
		defer delete(w.active, r)
		return w.make(n, path+"->"+r, depth+1)
	}
	if v, ok := m["const"]; ok {
		return []any{v}, nil
	}
	if v, ok := m["enum"].([]any); ok {
		if len(v) == 0 {
			return nil, fmt.Errorf("%s: empty enum", path)
		}
		return v, nil
	}
	for _, kind := range []string{"anyOf", "oneOf", "allOf"} {
		if alts, ok := m[kind].([]any); ok {
			if kind == "allOf" && len(alts) != 1 {
				return nil, fmt.Errorf("%s: unsupported multi-branch allOf", path)
			}
			var out []any
			for i, a := range alts {
				vs, err := w.make(a, fmt.Sprintf("%s/%s/%d", path, kind, i), depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, vs...)
			}
			// Alternatives are independent coverage obligations, not a Cartesian
			// expansion of one object. Keep the per-object limit while allowing the
			// complete bounded union to aggregate every alternative.
			if len(out) > 20000 {
				return nil, fmt.Errorf("%s: union witness bound exceeded", path)
			}
			return out, nil
		}
	}
	if ts, ok := m["type"].([]any); ok {
		var out []any
		for _, t := range ts {
			copy := cloneMap(m)
			copy["type"] = t
			v, e := w.make(copy, path+"/"+fmt.Sprint(t), depth+1)
			if e != nil {
				return nil, e
			}
			out = append(out, v...)
		}
		return bounded(out, path)
	}
	typ, _ := m["type"].(string)
	switch typ {
	case "null":
		return []any{nil}, nil
	case "boolean":
		return []any{true, false}, nil
	case "string":
		// A monotonic construction ID gives distinct property witnesses without
		// repeating long schema paths inside every enclosing aggregate payload.
		s := fmt.Sprintf("oracle_%d", w.count)
		if n, ok := number(m["minLength"]); ok && int(n) > len(s) {
			if n > 1000 {
				return nil, fmt.Errorf("%s: minLength exceeds bound", path)
			}
			s += strings.Repeat("x", int(n)-len(s))
		}
		if n, ok := number(m["maxLength"]); ok && int(n) < len(s) {
			if n < 0 {
				return nil, fmt.Errorf("%s: invalid maxLength", path)
			}
			s = s[:int(n)]
		}
		return []any{s}, nil
	case "number", "integer":
		h := fnv.New32a()
		_, _ = h.Write([]byte(path))
		chosen := json.Number(fmt.Sprint(h.Sum32()%100 + 1))
		value, _ := new(big.Rat).SetString(string(chosen))
		for _, bound := range []string{"minimum", "maximum"} {
			raw, exists := m[bound]
			if !exists {
				continue
			}
			n, ok := raw.(json.Number)
			if !ok {
				return nil, fmt.Errorf("%s: invalid numeric bound", path)
			}
			limit, ok := new(big.Rat).SetString(string(n))
			if !ok {
				return nil, fmt.Errorf("%s: unsupported numeric bound %s", path, n)
			}
			if typ == "integer" {
				whole := new(big.Int).Quo(limit.Num(), limit.Denom())
				if !limit.IsInt() {
					if bound == "minimum" && limit.Sign() > 0 {
						whole.Add(whole, big.NewInt(1))
					}
					if bound == "maximum" && limit.Sign() < 0 {
						whole.Sub(whole, big.NewInt(1))
					}
				}
				limit.SetInt(whole)
				n = json.Number(whole.String())
			}
			if bound == "minimum" && value.Cmp(limit) < 0 || bound == "maximum" && value.Cmp(limit) > 0 {
				chosen = n
				value.Set(limit)
			}
		}
		return []any{chosen}, nil
	case "array":
		item, ok := m["items"]
		if !ok {
			item = true
		}
		vs, err := w.make(item, path+"/items", depth+1)
		if err != nil {
			return nil, err
		}
		count := 1
		if min, ok := number(m["minItems"]); ok && min > 1 {
			count = int(min)
		}
		if count > 16 {
			return nil, fmt.Errorf("%s: minItems exceeds bound", path)
		}
		var out []any
		if min, _ := number(m["minItems"]); min == 0 {
			out = append(out, []any{})
		}
		if max, ok := number(m["maxItems"]); ok && max == 0 {
			return out, nil
		}
		for _, v := range vs {
			a := make([]any, count)
			for i := range a {
				a[i] = v
			}
			out = append(out, a)
		}
		return bounded(out, path)
	case "object":
		props, _ := m["properties"].(map[string]any)
		values := map[string][]any{}
		base := map[string]any{}
		for _, key := range keys(props) {
			vs, err := w.make(props[key], path+"/properties/"+key, depth+1)
			if err != nil {
				return nil, err
			}
			values[key] = vs
			base[key] = vs[0]
		}
		if len(props) == 0 {
			if ap, ok := m["additionalProperties"]; ok {
				if b, ok := ap.(bool); ok && !b {
					return []any{base}, nil
				}
				vs, err := w.make(ap, path+"/additionalProperties", depth+1)
				if err != nil {
					return nil, err
				}
				out := []any{base}
				for _, v := range vs {
					out = append(out, map[string]any{"oracle_key": v})
				}
				return bounded(out, path)
			}
			return []any{base, map[string]any{"oracle_future": map[string]any{"payload": []any{json.Number("17"), "value", nil}}}}, nil
		}
		out := []any{base}
		if additional, exists := m["additionalProperties"]; exists {
			if closed, isBool := additional.(bool); !isBool || closed {
				extras, err := w.make(additional, path+"/additionalProperties", depth+1)
				if err != nil {
					return nil, err
				}
				key := "__oracle_map_entry__"
				for {
					if _, exists := props[key]; !exists {
						break
					}
					key += "_"
				}
				for _, value := range extras {
					next := cloneMap(base)
					next[key] = value
					out = append(out, next)
				}
			}
		}
		// Vary one member at a time; never Cartesian-product branch alternatives.
		for _, key := range keys(props) {
			for _, v := range values[key][1:] {
				next := cloneMap(base)
				next[key] = v
				out = append(out, next)
			}
		}
		required := map[string]bool{}
		if rs, ok := m["required"].([]any); ok {
			for _, r := range rs {
				required[r.(string)] = true
			}
		}
		for _, key := range keys(props) {
			if !required[key] {
				next := cloneMap(base)
				delete(next, key)
				out = append(out, next)
			}
		}
		return bounded(out, path)
	case "":
		return freeValues(fmt.Sprint(w.count)), nil
	default:
		return nil, fmt.Errorf("%s: unsupported type %q", path, typ)
	}
}
func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, e := n.Float64()
	return f, e == nil
}
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func bounded(v []any, path string) ([]any, error) {
	if len(v) > 2048 {
		return nil, fmt.Errorf("%s: witness breadth bound exceeded", path)
	}
	return v, nil
}
func freeValues(path string) []any {
	return []any{nil, true, json.Number("37"), "oracle_" + path, []any{"a", json.Number("19")}, map[string]any{"oracle_key": "value"}}
}

// Each corruption is classified by the complete root schema. In an open anyOf
// union a malformed known variant can still be a valid unknown variant.
// Deduplicate the obligation (branch, field, value shape, corruption), not the
// whole valid witness: unrelated sibling alternatives must not multiply every
// field corruption. Every schema-valid witness is still retained separately.
// Materialize only unseen obligations: rebuilding an already-covered path adds
// no evidence and otherwise repeatedly clones all of its ancestors.
func mutations(v any, context string, seen map[string]bool, discriminators []string) ([]any, error) {
	var out []any
	visited := 0
	add := func(obligation string, materialize func() any) {
		key := context + "/" + obligation
		if !seen[key] {
			seen[key] = true
			out = append(out, materialize())
		}
	}
	var walk func(any, string, string, func(any) any, int) error
	walk = func(current any, path, branch string, rebuild func(any) any, depth int) error {
		visited++
		if depth > 40 || visited > 20000 {
			return fmt.Errorf("%s: mutation traversal bound exceeded", path)
		}
		if object, ok := current.(map[string]any); ok {
			for _, key := range discriminators {
				if value, exists := object[key]; exists {
					raw, _ := json.Marshal(value)
					branch += "/" + fmt.Sprintf("%q", key) + ":" + string(raw)
				}
			}
		}
		for i, bad := range []any{nil, false, json.Number("-1"), json.Number("0.5"), "", []any{}, map[string]any{}} {
			add(fmt.Sprintf("%s/%s/type/%d", branch, path, i), func() any { return rebuild(bad) })
		}
		switch object := current.(type) {
		case map[string]any:
			add(branch+"/"+path+"/unknown", func() any { extra := cloneMap(object); extra["__oracle_unknown_field__"] = true; return rebuild(extra) })
			for _, key := range keys(object) {
				nextPath := path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
				add(branch+"/"+nextPath+"/omit", func() any { missing := cloneMap(object); delete(missing, key); return rebuild(missing) })
				childRebuild := func(value any) any { copy := cloneMap(object); copy[key] = value; return rebuild(copy) }
				if err := walk(object[key], nextPath, branch, childRebuild, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for i, value := range object {
				nextPath := fmt.Sprintf("%s/%d", path, i)
				childRebuild := func(value any) any { copy := append([]any{}, object...); copy[i] = value; return rebuild(copy) }
				if err := walk(value, nextPath, branch, childRebuild, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := walk(v, "#", "", func(value any) any { return value }, 0)
	return out, err
}

// terminal closes a recursive edge with a schema-valid finite base case. The
// ordinary pass already visits all fields and alternatives once; this helper
// does not claim unbounded recursive-instance coverage.
func (w *witnesses) terminal(node any, path string, seen map[string]bool, depth int) (any, error) {
	if depth > 30 {
		return nil, fmt.Errorf("%s: no bounded recursive base case", path)
	}
	if b, ok := node.(bool); ok {
		if b {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: false base case", path)
	}
	m := node.(map[string]any)
	if r, ok := m["$ref"].(string); ok {
		if seen[r] {
			return nil, fmt.Errorf("%s: required recursive cycle", path)
		}
		seen[r] = true
		defer delete(seen, r)
		n, e := pointer(w.doc, r)
		if e != nil {
			return nil, e
		}
		return w.terminal(n, path+"->"+r, seen, depth+1)
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if a, ok := m[key].([]any); ok {
			for _, n := range a {
				v, e := w.terminal(n, path+"/"+key, seen, depth+1)
				if e == nil {
					return v, nil
				}
			}
			return nil, fmt.Errorf("%s: union has no finite base case", path)
		}
	}
	if a, ok := m["type"].([]any); ok {
		for _, typ := range a {
			if typ == "null" {
				return nil, nil
			}
		}
		for _, typ := range a {
			n := cloneMap(m)
			n["type"] = typ
			v, e := w.terminal(n, path, seen, depth+1)
			if e == nil {
				return v, nil
			}
		}
		return nil, fmt.Errorf("%s: no finite type alternative", path)
	}
	switch m["type"] {
	case "array":
		if min, _ := number(m["minItems"]); min == 0 {
			return []any{}, nil
		}
	case "object":
		out := map[string]any{}
		props, _ := m["properties"].(map[string]any)
		if rs, ok := m["required"].([]any); ok {
			for _, r := range rs {
				k := r.(string)
				n, ok := props[k]
				if !ok {
					return nil, fmt.Errorf("%s: required property %s undeclared", path, k)
				}
				v, e := w.terminal(n, path+"/"+k, seen, depth+1)
				if e != nil {
					return nil, e
				}
				out[k] = v
			}
		}
		return out, nil
	}
	vs, e := w.make(node, path, depth+1)
	if e != nil {
		return nil, e
	}
	return vs[0], nil
}

package schemaoracle

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The previous eager algorithm is a test-only equivalence oracle. It is not
// used to choose production expectations or coverage.
func eagerReferenceMutations(v any, context string, seen map[string]bool, discriminators []string) ([]any, error) {
	var out []any
	visited := 0
	add := func(obligation string, value any) {
		key := context + "/" + obligation
		if !seen[key] {
			seen[key] = true
			out = append(out, value)
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
			add(fmt.Sprintf("%s/%s/type/%d", branch, path, i), rebuild(bad))
		}
		switch object := current.(type) {
		case map[string]any:
			extra := cloneMap(object)
			extra["__oracle_unknown_field__"] = true
			add(branch+"/"+path+"/unknown", rebuild(extra))
			for _, key := range keys(object) {
				nextPath := path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
				missing := cloneMap(object)
				delete(missing, key)
				add(branch+"/"+nextPath+"/omit", rebuild(missing))
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

func TestLazyMutationsPreserveCompleteCorpusAndObligations(t *testing.T) {
	values := []any{
		map[string]any{"kind": "first", "outer": map[string]any{"inner": map[string]any{"value": "one", "optional": nil}}, "list": []any{map[string]any{"type": "alpha", "x": json.Number("3")}}},
		map[string]any{"kind": "first", "outer": map[string]any{"inner": map[string]any{"value": "two"}}, "list": []any{map[string]any{"type": "beta", "x": "value"}}},
		map[string]any{"kind": "second", "outer": map[string]any{"inner": map[string]any{"value": "three"}}, "list": []any{map[string]any{"type": "alpha", "x": json.Number("3")}}},
		map[string]any{"a/b": map[string]any{"~key": "value"}, "extra": []any{nil, true, json.Number("1.5")}},
	}
	eagerSeen, lazySeen := map[string]bool{}, map[string]bool{}
	var eagerCorpus, lazyCorpus []any
	for repeat := 0; repeat < 3; repeat++ {
		for _, value := range values {
			eager, err := eagerReferenceMutations(value, "target", eagerSeen, []string{"kind", "type"})
			if err != nil {
				t.Fatal(err)
			}
			lazy, err := mutations(value, "target", lazySeen, []string{"kind", "type"})
			if err != nil {
				t.Fatal(err)
			}
			eagerCorpus = append(eagerCorpus, eager...)
			for _, mutation := range lazy {
				lazyCorpus = append(lazyCorpus, mutation.Value)
			}
		}
	}
	expected, _ := json.Marshal(eagerCorpus)
	actual, _ := json.Marshal(lazyCorpus)
	if sha256.Sum256(expected) != sha256.Sum256(actual) {
		t.Fatal("lazy materialization changed payloads or ordering")
	}
	if !reflect.DeepEqual(eagerSeen, lazySeen) {
		t.Fatal("lazy materialization changed path/branch obligations")
	}
	if len(lazyCorpus) != len(lazySeen) {
		t.Fatalf("repeated obligations were rematerialized: corpus=%d obligations=%d", len(lazyCorpus), len(lazySeen))
	}
	deep := false
	for key := range lazySeen {
		if strings.Contains(key, "#/outer/inner/value/omit") {
			deep = true
		}
	}
	if !deep {
		t.Fatal("deep required-field obligation lost")
	}
	t.Logf("equivalent mutation corpus: %d probes, SHA-256 %x", len(lazyCorpus), sha256.Sum256(actual))
}

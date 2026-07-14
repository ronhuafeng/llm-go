package llmschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestSchemaJSONForIsDeterministicForRepresentativeTypes(t *testing.T) {
	tests := []struct {
		name     string
		generate func() (json.RawMessage, error)
	}{
		{name: "scalar", generate: SchemaJSONFor[bool]},
		{name: "small flat", generate: SchemaJSONFor[benchmarkSmall]},
		{name: "nested slices", generate: SchemaJSONFor[benchmarkNested]},
		{name: "nullable pointer heavy", generate: SchemaJSONFor[benchmarkNullable]},
		{name: "maps and raw message", generate: SchemaJSONFor[benchmarkMapRaw]},
		{name: "realistic code review", generate: SchemaJSONFor[benchmarkReviewReport]},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, err := test.generate()
			if err != nil {
				t.Fatal(err)
			}
			for range 100 {
				next, err := test.generate()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(next, first) {
					t.Fatalf("schema bytes changed:\nfirst: %s\n next: %s", first, next)
				}
			}
		})
	}
}

func TestReflectTypeCacheKeyCharacteristics(t *testing.T) {
	type named benchmarkSmall
	type alias = benchmarkSmall
	type genericStringAlias = benchmarkGeneric[string]

	smallType := reflect.TypeFor[benchmarkSmall]()
	if got := reflect.TypeFor[alias](); got != smallType {
		t.Fatalf("alias type = %v, want canonical %v", got, smallType)
	}
	if got := reflect.TypeFor[named](); got == smallType {
		t.Fatalf("distinct named type unexpectedly shares key %v", got)
	}
	if got := reflect.TypeFor[*benchmarkSmall](); got == smallType {
		t.Fatalf("pointer type unexpectedly shares value key %v", got)
	}
	if got := reflect.TypeFor[struct {
		Name  string `json:"name"`
		Score int    `json:"score"`
	}](); got == smallType {
		t.Fatalf("anonymous type unexpectedly shares named key %v", got)
	}
	genericType := reflect.TypeFor[benchmarkGeneric[string]]()
	if got := reflect.TypeFor[genericStringAlias](); got != genericType {
		t.Fatalf("instantiated generic alias type = %v, want canonical %v", got, genericType)
	}
	if got := reflect.TypeFor[benchmarkGeneric[int]](); got == genericType {
		t.Fatalf("different generic instantiations unexpectedly share key %v", got)
	}
}

type benchmarkSmall struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
}

type benchmarkNested struct {
	Title string           `json:"title"`
	Items []benchmarkSmall `json:"items"`
}

type benchmarkNullable struct {
	Name   *string            `json:"name,omitempty"`
	Count  *int               `json:"count,omitempty"`
	Active *bool              `json:"active,omitempty"`
	Tags   []*string          `json:"tags"`
	Meta   map[string]*string `json:"meta"`
}

type benchmarkMapRaw struct {
	Labels   map[string]int  `json:"labels"`
	Evidence json.RawMessage `json:"evidence"`
}

type benchmarkRecursive struct {
	Value    string               `json:"value"`
	Children []benchmarkRecursive `json:"children,omitempty"`
}

type benchmarkGeneric[T any] struct {
	Items []T `json:"items"`
}

type benchmarkReviewFinding struct {
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Severity   string   `json:"severity"`
	Summary    string   `json:"summary"`
	Evidence   []string `json:"evidence"`
	Suggestion *string  `json:"suggestion,omitempty"`
}

type benchmarkReviewReport struct {
	Summary  string                   `json:"summary"`
	Findings []benchmarkReviewFinding `json:"findings"`
	Metrics  map[string]int           `json:"metrics"`
	Raw      json.RawMessage          `json:"raw,omitempty"`
}

var (
	benchmarkValueSink  any
	benchmarkBytesSink  json.RawMessage
	benchmarkSchemaSink *validator.Schema
	benchmarkErrorSink  error
)

func BenchmarkSchemaPipeline(b *testing.B) {
	benchmarkPipeline[bool](b, "scalar", []byte(`true`))
	benchmarkPipeline[benchmarkSmall](b, "small-flat", []byte(`{"name":"pass","score":2}`))
	benchmarkPipeline[benchmarkNested](b, "nested-slices", []byte(`{"title":"batch","items":[{"name":"a","score":1},{"name":"b","score":2}]}`))
	benchmarkPipeline[benchmarkNullable](b, "nullable-pointer-heavy", []byte(`{"name":"sample","count":3,"active":true,"tags":["a",null],"meta":{"owner":"team","note":null}}`))
	benchmarkPipeline[benchmarkMapRaw](b, "maps-raw-message", []byte(`{"labels":{"a":1,"b":2},"evidence":{"source":"synthetic","ok":true}}`))
	benchmarkPipeline[benchmarkReviewReport](b, "realistic-code-review", benchmarkReviewFixture())
}

func benchmarkReviewFixture() []byte {
	return []byte(`{
		"summary":"Review of a provider-neutral Go change with compatibility and concurrency checks.",
		"findings":[
			{"file":"llmschema/schema.go","line":56,"severity":"high","summary":"Repeated schema compilation dominates typed decode.","evidence":["profile: compile","benchmark: nested"],"suggestion":"Measure before changing production behavior."},
			{"file":"llmadapter/adapter.go","line":103,"severity":"medium","summary":"Preserve provider-neutral ownership semantics.","evidence":["contract test","race test"]},
			{"file":"docs/release.md","line":67,"severity":"low","summary":"Keep race verification aligned with release guidance.","evidence":["CI job","release checklist"]}
		],
		"metrics":{"files":3,"tests":14,"allocations":1225},
		"raw":{"reviewer":"synthetic-realistic","version":1}
	}`)
}

func benchmarkPipeline[T any](b *testing.B, name string, data []byte) {
	b.Helper()
	b.Run(name, func(b *testing.B) {
		schemaJSON, err := SchemaJSONFor[T]()
		if err != nil {
			b.Fatalf("generate schema: %v", err)
		}
		instance, err := decodeJSON(data)
		if err != nil {
			b.Fatalf("decode fixture: %v", err)
		}
		compiled, err := compileBenchmarkSchema(schemaJSON)
		if err != nil {
			b.Fatalf("compile schema: %v", err)
		}

		b.Run("generate", func(b *testing.B) {
			b.ReportAllocs()
			var err error
			for i := 0; i < b.N; i++ {
				benchmarkBytesSink, err = SchemaJSONFor[T]()
			}
			if err != nil {
				b.Fatal(err)
			}
		})
		b.Run("compile", func(b *testing.B) {
			b.ReportAllocs()
			var err error
			for i := 0; i < b.N; i++ {
				benchmarkSchemaSink, err = compileBenchmarkSchema(schemaJSON)
			}
			if err != nil {
				b.Fatal(err)
			}
		})
		b.Run("validate", func(b *testing.B) {
			b.ReportAllocs()
			var err error
			for i := 0; i < b.N; i++ {
				err = compiled.Validate(instance)
			}
			if err != nil {
				b.Fatal(err)
			}
		})
		b.Run("unmarshal", func(b *testing.B) {
			b.ReportAllocs()
			var err error
			for i := 0; i < b.N; i++ {
				var value T
				err = json.Unmarshal(data, &value)
				benchmarkValueSink = value
			}
			if err != nil {
				b.Fatal(err)
			}
		})
		b.Run("decode", func(b *testing.B) {
			b.ReportAllocs()
			var err error
			for i := 0; i < b.N; i++ {
				benchmarkValueSink, err = Decode[T](data)
			}
			if err != nil {
				b.Fatal(err)
			}
		})
	})
}

func BenchmarkDecodeRepeatedType(b *testing.B) {
	data := []byte(`{"title":"batch","items":[{"name":"a","score":1},{"name":"b","score":2}]}`)
	b.ReportAllocs()
	var err error
	for i := 0; i < b.N; i++ {
		benchmarkValueSink, err = Decode[benchmarkNested](data)
	}
	if err != nil {
		b.Fatal(err)
	}
}

func BenchmarkDecodeDistinctTypes(b *testing.B) {
	decoders := []func() error{
		func() error { _, err := Decode[bool]([]byte(`true`)); return err },
		func() error { _, err := Decode[string]([]byte(`"ok"`)); return err },
		func() error { _, err := Decode[[]int]([]byte(`[1,2,3]`)); return err },
		func() error { _, err := Decode[benchmarkSmall]([]byte(`{"name":"a","score":1}`)); return err },
		func() error { _, err := Decode[benchmarkNested]([]byte(`{"title":"batch","items":[]}`)); return err },
		func() error { _, err := Decode[benchmarkNullable]([]byte(`{"tags":[],"meta":{}}`)); return err },
		func() error { _, err := Decode[benchmarkMapRaw]([]byte(`{"labels":{},"evidence":null}`)); return err },
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, decode := range decoders {
			if err := decode(); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkSchemaRecursiveSupport(b *testing.B) {
	if _, err := SchemaJSONFor[benchmarkRecursive](); err != nil {
		b.Skipf("recursive schema generation is unsupported: %v", err)
	}
	b.Fatal("recursive schema generation is now supported; add it to the pipeline benchmarks")
}

func BenchmarkDecodeParallelSameType(b *testing.B) {
	data := []byte(`{"title":"batch","items":[{"name":"a","score":1},{"name":"b","score":2}]}`)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := Decode[benchmarkNested](data); err != nil {
				b.Error(err)
			}
		}
	})
}

func BenchmarkCompiledValidationParallel(b *testing.B) {
	data := []byte(`{"title":"batch","items":[{"name":"a","score":1},{"name":"b","score":2}]}`)
	schemaJSON, err := SchemaJSONFor[benchmarkNested]()
	if err != nil {
		b.Fatal(err)
	}
	compiled, err := compileBenchmarkSchema(schemaJSON)
	if err != nil {
		b.Fatal(err)
	}
	instance, err := decodeJSON(data)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := compiled.Validate(instance); err != nil {
				b.Error(err)
			}
		}
	})
}

func BenchmarkDecodeSchemaViolation(b *testing.B) {
	data := []byte(`{"name":7,"score":"bad"}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := Decode[benchmarkSmall](data)
		if err == nil {
			b.Fatal("Decode accepted schema violation")
		}
		benchmarkErrorSink = err
	}
}

func compileBenchmarkSchema(schemaJSON json.RawMessage) (*validator.Schema, error) {
	var document any
	decoder := json.NewDecoder(bytes.NewReader(schemaJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode generated schema: %w", err)
	}
	compiler := validator.NewCompiler()
	const schemaURL = "https://llmkit.local/benchmark-output-schema.json"
	if err := compiler.AddResource(schemaURL, document); err != nil {
		return nil, fmt.Errorf("register generated schema: %w", err)
	}
	return compiler.Compile(schemaURL)
}

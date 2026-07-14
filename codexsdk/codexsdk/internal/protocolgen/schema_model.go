package protocolgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CoverageMatrix struct {
	Fields []CoverageField `json:"fields"`
	Status string          `json:"status"`
	Types  []CoverageType  `json:"types"`
}

type CoverageType struct {
	Schema    string `json:"schema"`
	Stability string `json:"stability"`
	Status    string `json:"status"`
	Type      string `json:"type"`
}

type CoverageField struct {
	Field     string `json:"field"`
	Path      string `json:"path"`
	Required  bool   `json:"required"`
	Schema    string `json:"schema"`
	Stability string `json:"stability"`
	Status    string `json:"status"`
	Type      string `json:"type"`
}

func LoadCoverageMatrix(path string) (CoverageMatrix, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return CoverageMatrix{}, err
	}
	var matrix CoverageMatrix
	if err := json.Unmarshal(raw, &matrix); err != nil {
		return CoverageMatrix{}, fmt.Errorf("decode coverage matrix: %w", err)
	}
	if matrix.Status != classifiedManifestStatus {
		return CoverageMatrix{}, fmt.Errorf("coverage matrix status %q is not %q", matrix.Status, classifiedManifestStatus)
	}
	if len(matrix.Types) == 0 {
		return CoverageMatrix{}, fmt.Errorf("coverage matrix has no types")
	}
	seenTypes := map[string]bool{}
	for _, typ := range matrix.Types {
		if typ.Schema == "" || typ.Type == "" || typ.Status == "" || typ.Stability == "" {
			return CoverageMatrix{}, fmt.Errorf("coverage type %q is missing required facts", typ.Schema)
		}
		if seenTypes[typ.Schema] {
			return CoverageMatrix{}, fmt.Errorf("coverage type schema %q appears more than once", typ.Schema)
		}
		seenTypes[typ.Schema] = true
	}
	for _, field := range matrix.Fields {
		if field.Schema == "" || field.Type == "" || field.Field == "" || field.Path == "" || field.Status == "" || field.Stability == "" {
			return CoverageMatrix{}, fmt.Errorf("coverage field %q is missing required facts", field.Path)
		}
		if !seenTypes[field.Schema] {
			return CoverageMatrix{}, fmt.Errorf("coverage field %q references unknown type schema %q", field.Path, field.Schema)
		}
	}
	return matrix, nil
}

type SchemaFile struct {
	Path      string
	TypeName  string
	Stability string
	Schema    *Schema
}

func LoadCoverageSchemas(root string, matrix CoverageMatrix) ([]SchemaFile, error) {
	files := make([]SchemaFile, 0, len(matrix.Types))
	for _, typ := range matrix.Types {
		schema, err := LoadSchema(filepath.Join(root, filepath.FromSlash(typ.Schema)))
		if err != nil {
			return nil, fmt.Errorf("load schema %s: %w", typ.Schema, err)
		}
		files = append(files, SchemaFile{
			Path:      typ.Schema,
			TypeName:  typ.Type,
			Stability: typ.Stability,
			Schema:    schema,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func LoadSchema(path string) (*Schema, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var schema Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode schema: %w", err)
	}
	return &schema, nil
}

type Schema struct {
	Bool                 *bool
	AdditionalProperties AdditionalProperties
	AllOf                []*Schema
	AnyOf                []*Schema
	Default              SchemaKeywordPresence
	Definitions          map[string]*Schema
	Description          string
	Enum                 []string
	Format               string
	Items                *Schema
	Minimum              *float64
	MinItems             *uint64
	OneOf                []*Schema
	Properties           map[string]*Schema
	Ref                  string
	Required             []string
	Title                string
	Type                 SchemaTypeSet
	UnknownKeywords      []string
}

func (s *Schema) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	switch trimmed {
	case "true":
		value := true
		*s = Schema{Bool: &value}
		return nil
	case "false":
		value := false
		*s = Schema{Bool: &value}
		return nil
	}

	var raw map[string]schemaKeywordValue
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	type schemaObject struct {
		AdditionalProperties AdditionalProperties  `json:"additionalProperties"`
		AllOf                []*Schema             `json:"allOf"`
		AnyOf                []*Schema             `json:"anyOf"`
		Default              SchemaKeywordPresence `json:"default"`
		Definitions          map[string]*Schema    `json:"definitions"`
		Description          string                `json:"description"`
		Enum                 []string              `json:"enum"`
		Format               string                `json:"format"`
		Items                *Schema               `json:"items"`
		Minimum              *float64              `json:"minimum"`
		MinItems             *uint64               `json:"minItems"`
		OneOf                []*Schema             `json:"oneOf"`
		Properties           map[string]*Schema    `json:"properties"`
		Ref                  string                `json:"$ref"`
		Required             []string              `json:"required"`
		Title                string                `json:"title"`
		Type                 SchemaTypeSet         `json:"type"`
	}
	var object schemaObject
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	s.Bool = nil
	s.AdditionalProperties = object.AdditionalProperties
	s.AllOf = object.AllOf
	s.AnyOf = object.AnyOf
	s.Default = object.Default
	s.Definitions = object.Definitions
	s.Description = object.Description
	s.Enum = object.Enum
	s.Format = object.Format
	s.Items = object.Items
	s.Minimum = object.Minimum
	s.MinItems = object.MinItems
	s.OneOf = object.OneOf
	s.Properties = object.Properties
	s.Ref = object.Ref
	s.Required = append([]string(nil), object.Required...)
	s.Title = object.Title
	s.Type = object.Type
	s.UnknownKeywords = unknownSchemaKeywords(raw)
	return nil
}

func (s *Schema) IsTrueSchema() bool {
	return s != nil && s.Bool != nil && *s.Bool
}

func (s *Schema) IsFalseSchema() bool {
	return s != nil && s.Bool != nil && !*s.Bool
}

func (s *Schema) RequiredSet() map[string]bool {
	required := map[string]bool{}
	for _, name := range s.Required {
		required[name] = true
	}
	return required
}

type SchemaTypeSet struct {
	Values []string
}

func (s *SchemaTypeSet) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		s.Values = nil
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		s.Values = []string{single}
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	s.Values = values
	return nil
}

func (s SchemaTypeSet) Has(value string) bool {
	for _, candidate := range s.Values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (s SchemaTypeSet) Only(value string) bool {
	return len(s.Values) == 1 && s.Values[0] == value
}

func (s SchemaTypeSet) NonNullValues() []string {
	values := make([]string, 0, len(s.Values))
	for _, value := range s.Values {
		if value != "null" {
			values = append(values, value)
		}
	}
	return values
}

func (s SchemaTypeSet) NullableSingle() (string, bool) {
	if len(s.Values) != 2 || !s.Has("null") {
		return "", false
	}
	values := s.NonNullValues()
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}

type AdditionalProperties struct {
	Bool    *bool
	Present bool
	Schema  *Schema
}

func (a *AdditionalProperties) UnmarshalJSON(data []byte) error {
	a.Present = true
	trimmed := strings.TrimSpace(string(data))
	switch trimmed {
	case "true":
		value := true
		a.Bool = &value
		a.Schema = nil
		return nil
	case "false":
		value := false
		a.Bool = &value
		a.Schema = nil
		return nil
	}
	var schema Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return err
	}
	a.Bool = nil
	a.Schema = &schema
	return nil
}

func refTypeName(ref string) string {
	if ref == "" {
		return ""
	}
	return ref[strings.LastIndex(ref, "/")+1:]
}

type schemaKeywordValue []byte

func (s *schemaKeywordValue) UnmarshalJSON(data []byte) error {
	*s = append((*s)[:0], data...)
	return nil
}

type SchemaKeywordPresence struct {
	Present bool
}

func (s *SchemaKeywordPresence) UnmarshalJSON(data []byte) error {
	s.Present = true
	return nil
}

func unknownSchemaKeywords(raw map[string]schemaKeywordValue) []string {
	known := map[string]bool{
		"$ref":                 true,
		"$schema":              true,
		"additionalProperties": true,
		"allOf":                true,
		"anyOf":                true,
		"default":              true,
		"definitions":          true,
		"description":          true,
		"enum":                 true,
		"items":                true,
		"oneOf":                true,
		"properties":           true,
		"required":             true,
		"title":                true,
		"type":                 true,
		"writeOnly":            true,
	}
	var unknown []string
	for keyword := range raw {
		if !known[keyword] {
			unknown = append(unknown, keyword)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func unmodeledKeywords(schema *Schema) []string {
	seen := map[string]bool{}
	var collect func(*Schema)
	collect = func(current *Schema) {
		if current == nil || current.Bool != nil {
			return
		}
		for _, keyword := range current.UnknownKeywords {
			seen[keyword] = true
		}
		collect(current.Items)
		if current.AdditionalProperties.Schema != nil {
			collect(current.AdditionalProperties.Schema)
		}
		for _, child := range current.AllOf {
			collect(child)
		}
		for _, child := range current.AnyOf {
			collect(child)
		}
		for _, child := range current.OneOf {
			collect(child)
		}
	}
	collect(schema)
	var out []string
	for keyword := range seen {
		out = append(out, keyword)
	}
	sort.Strings(out)
	return out
}

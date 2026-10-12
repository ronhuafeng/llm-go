package schemaoracle

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// Freeze only a compact explanation, never the validator's potentially wide
// union error tree. This is feedback, not an additional acceptance decision.
func invalidDiagnostic(original any, name string, value any, mutationPath string, err error) string {
	fallback := "instance=" + diagnosticClip(mutationPath, 160) + " validation failed; attribution ambiguous"
	root, ok := err.(*validator.ValidationError)
	if !ok {
		return fallback
	}
	var selected *validator.ValidationError
	source := ""
	union, sampled, ambiguous, visited := false, false, false, 0
	var walk func(*validator.ValidationError)
	walk = func(e *validator.ValidationError) {
		visited++
		if visited > 20000 {
			ambiguous = true
			return
		}
		switch e.ErrorKind.(type) {
		case *kind.AnyOf, *kind.OneOf:
			union = true
		}
		if len(e.Causes) > 0 {
			for _, child := range e.Causes {
				if visited > 20000 {
					break
				}
				walk(child)
			}
			return
		}
		instance := "#" + diagnosticPointer(e.InstanceLocation)
		// Required errors point at the owning object, not the omitted member.
		matches := instance == mutationPath
		if required, ok := e.ErrorKind.(*kind.Required); ok {
			for _, missing := range required.Missing {
				if instance+diagnosticPointer([]string{missing}) == mutationPath {
					matches = true
				}
			}
		}
		if !matches {
			return
		}
		const prefix = "https://schema-oracle.invalid/target.json"
		if !strings.HasPrefix(e.SchemaURL, prefix+"#") {
			ambiguous = true
			return
		}
		fragment, decodeErr := url.PathUnescape(strings.TrimPrefix(e.SchemaURL, prefix))
		if decodeErr != nil {
			ambiguous = true
			return
		}
		location := fragment + diagnosticPointer(e.ErrorKind.KeywordPath())
		if selected != nil && location != source {
			sampled = true // Multiple matching locations are sampled, never a unique cause.
			_, priorType := selected.ErrorKind.(*kind.Type)
			_, nextType := e.ErrorKind.(*kind.Type)
			// Prefer a specific constraint over a broad type mismatch, then
			// sort by owning keyword so validator map iteration cannot select
			// a different sampled cause on another construction.
			if (nextType && !priorType) || (nextType == priorType && location > source) {
				return
			}
		}
		selected, source = e, location
	}
	walk(root)
	if selected == nil || ambiguous {
		if visited > 20000 {
			return fallback + "; diagnostic traversal truncated"
		}
		return fallback
	}
	label := "source="
	// An absent keyword comes from an overlay or schema normalization.
	// Neither may impersonate an exact upstream keyword location.
	if _, err := diagnosticLookup(original, source); err != nil {
		switch selected.ErrorKind.(type) {
		case *kind.AdditionalProperties, *kind.Format:
			label = "local-overlay="
		default:
			label = "normalized-schema="
			sampled = true
		}
	}
	message := "constraint failed (value omitted)"
	switch selected.ErrorKind.(type) {
	case *kind.Type, *kind.MinLength, *kind.MaxLength, *kind.MinItems, *kind.MaxItems, *kind.Minimum, *kind.Maximum:
		message = selected.BasicOutput().Error.String()
	}
	at := "#" + diagnosticPointer(selected.InstanceLocation)
	previewValue, lookupErr := diagnosticLookup(value, mutationPath)
	preview := "<missing>"
	if lookupErr == nil {
		preview = diagnosticValue(previewValue)
	}
	suffix := ""
	if union {
		suffix = "; union sampled cause; attribution ambiguous"
	} else if sampled {
		suffix = "; sampled cause; attribution ambiguous"
	}
	return label + diagnosticClip(name+source, 192) + " instance=" + diagnosticClip(at, 160) + " " + diagnosticClip(message, 120) + " value=" + preview + suffix
}

func diagnosticPointer(tokens []string) string {
	var b strings.Builder
	for _, token := range tokens {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

func diagnosticClip(s string, limit int) string {
	if len(s) > limit {
		return s[:limit] + "...[truncated]"
	}
	return s
}

// Do not slice request bodies, URL credentials, or tokens into partial secrets
// which the controller's exact-value redactor could no longer recognize.
func diagnosticValue(value any) string {
	switch v := value.(type) {
	case string:
		if v == "" {
			return `""`
		}
		return fmt.Sprintf("<string bytes=%d>", len(v))
	case map[string]any:
		return fmt.Sprintf("<object members=%d>", len(v))
	case []any:
		return fmt.Sprintf("<array items=%d>", len(v))
	default:
		raw, _ := json.Marshal(value)
		return diagnosticClip(string(raw), 48)
	}
}

// Unlike local schema references, diagnostic instance/keyword pointers can
// traverse array indices (including anyOf/oneOf schema branches).
func diagnosticLookup(value any, pointer string) (any, error) {
	if pointer == "#" {
		return value, nil
	}
	if !strings.HasPrefix(pointer, "#/") {
		return nil, fmt.Errorf("invalid pointer")
	}
	for _, token := range strings.Split(pointer[2:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[token]
			if !ok {
				return nil, fmt.Errorf("missing")
			}
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(v) {
				return nil, fmt.Errorf("missing")
			}
			value = v[i]
		default:
			return nil, fmt.Errorf("missing")
		}
	}
	return value, nil
}

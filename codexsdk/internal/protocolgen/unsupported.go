package protocolgen

import (
	"errors"
	"fmt"
	"strings"
)

// UnsupportedSchemaError identifies a concrete schema representation that the
// current generator cannot preserve. Filesystem and source-integrity errors
// must not be wrapped in this type.
type UnsupportedSchemaError struct {
	Path string
	Err  error
}

func (e *UnsupportedSchemaError) Error() string {
	return fmt.Sprintf("unsupported schema at %s: %v", e.Path, e.Err)
}

func (e *UnsupportedSchemaError) Unwrap() error { return e.Err }

func unsupportedGeneratedSchema(path, format string, args ...any) error {
	return &UnsupportedSchemaError{Path: path, Err: fmt.Errorf(format, args...)}
}

func unsupportedDefinitionPath(schemaPath, name string) string {
	name = strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1")
	return schemaPath + "#/definitions/" + name
}

func classifyGeneratedSchemaError(path string, err error) error {
	var unsupported *UnsupportedSchemaError
	if errors.As(err, &unsupported) {
		return err
	}
	return &UnsupportedSchemaError{Path: path, Err: err}
}

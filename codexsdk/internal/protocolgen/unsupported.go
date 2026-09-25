package protocolgen

import "fmt"

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

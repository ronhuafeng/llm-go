package protocolv2

import (
	"bytes"
	"encoding/json"
)

// Nullable represents a present field that may either be JSON null or a value.
// Use *Nullable[T] on surrounding struct fields: nil field pointer means omit.
type Nullable[T any] struct {
	Value *T
}

// Null constructs a present nullable field that marshals as JSON null.
func Null[T any]() *Nullable[T] {
	return &Nullable[T]{}
}

// Value constructs a present nullable field that marshals as a concrete value.
func Value[T any](value T) *Nullable[T] {
	return &Nullable[T]{Value: &value}
}

// MarshalJSON marshals nil nullable values as JSON null and concrete values as T.
func (value Nullable[T]) MarshalJSON() ([]byte, error) {
	if value.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*value.Value)
}

// UnmarshalJSON decodes a nullable value in contexts where field omission is
// not represented by the Nullable itself, such as typed map values.
func (value *Nullable[T]) UnmarshalJSON(data []byte) error {
	decoded, err := NullableFromJSON[T](data)
	if err != nil {
		return err
	}
	*value = *decoded
	return nil
}

// NullableFromJSON decodes a present nullable field. Generated struct decoders
// must call this only when the surrounding field key is present; ordinary
// json.Unmarshal into *Nullable[T] cannot distinguish an absent key from an
// explicit JSON null.
func NullableFromJSON[T any](data []byte) (*Nullable[T], error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return Null[T](), nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return Value(value), nil
}

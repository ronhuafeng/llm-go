package llmadapter

// Observation is a toolkit-owned fact that may be unknown. The zero value is
// unknown and does not manufacture a value. Requested settings, defaults,
// estimates, heuristics, and inferred names cannot populate an observation;
// only an explicit Observed constructor records a fact.
type Observation[T any] struct {
	value   T
	present bool
}

// Observed records an established fact. It is the only way to mark a value
// present.
func Observed[T any](value T) Observation[T] {
	return Observation[T]{value: value, present: true}
}

// Present reports whether a fact was established.
func (o Observation[T]) Present() bool {
	return o.present
}

// Value returns the observed fact and whether it is present. If the
// observation is unknown, value is the type's zero value.
func (o Observation[T]) Value() (T, bool) {
	return o.value, o.present
}

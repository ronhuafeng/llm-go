package protocolupgrade

// SourceIntegrityError means the supplied source identities or facts disagree.
// It is not an unsupported representation and cannot authorize an Agent repair.
type SourceIntegrityError struct{ Err error }

func (e *SourceIntegrityError) Error() string { return e.Err.Error() }
func (e *SourceIntegrityError) Unwrap() error { return e.Err }

// VerificationError means reconstructed artifacts disagree with the repository.
type VerificationError struct{ Err error }

func (e *VerificationError) Error() string { return e.Err.Error() }
func (e *VerificationError) Unwrap() error { return e.Err }

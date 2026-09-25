package protocolsync

import (
	"errors"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolupgrade"
	"os"
	"os/exec"
)

const (
	FailureUnsupported = "unsupported_representation"
	FailureSource      = "source_integrity"
	FailureExecution   = "execution_environment"
	FailureValidation  = "repository_validation"
	FailurePublication = "publication_conflict"
	FailurePolicy      = "policy_configuration"
	FailureUnknown     = "unknown"
)

// Failure carries owner-assigned attribution without changing the underlying cause.
// Diagnostic paths and error text never authorize a repair.
type Failure struct {
	Category string
	Err      error
}

func (e *Failure) Error() string { return e.Err.Error() }
func (e *Failure) Unwrap() error { return e.Err }

func failureCategory(err error) string {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.Category
	}
	var path *os.PathError
	var unsupported *protocolupgrade.IncompatibilityError
	if errors.As(err, &unsupported) {
		return FailureUnsupported
	}
	var source *protocolupgrade.SourceIntegrityError
	if errors.As(err, &source) {
		return FailureSource
	}
	var verification *protocolupgrade.VerificationError
	if errors.As(err, &verification) {
		return FailureValidation
	}
	var launch *exec.Error
	if errors.As(err, &path) || errors.As(err, &launch) || errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return FailureExecution
	}
	return FailureUnknown
}

func finishSync(result *SyncResult, err error, stage string) {
	result.Stage = stage
	if err != nil {
		result.Outcome = OutcomeFailed
		result.FailureCategory = failureCategory(err)
		result.Reason = err.Error()
	}
}

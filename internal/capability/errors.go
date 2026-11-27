package capability

import (
	"context"
	"errors"
	"net"
)

// Fault adds a stable code and recovery hint while preserving the original cause.
type Fault struct {
	Code, Message, Hint string
	Cause               error
}

func (e *Fault) Error() string { return e.Message }
func (e *Fault) Unwrap() error { return e.Cause }
func NewFault(code, message, hint string, cause error) error {
	return &Fault{Code: code, Message: message, Hint: hint, Cause: cause}
}
func InvalidArgument(message string) error {
	return NewFault("INVALID_ARGUMENT", message, "Inspect --help or describe the operation, then correct the input.", nil)
}
func ErrorDetails(err error) (code, hint string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "TIMEOUT", "The outcome may be unknown. Verify it before repeating a mutation."
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELLED", "The outcome may be unknown. Verify it before repeating a mutation."
	}
	var fault *Fault
	if errors.As(err, &fault) {
		return fault.Code, fault.Hint
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "TIMEOUT", "The outcome may be unknown. Verify it before repeating a mutation."
		}
		return "PROVIDER_UNAVAILABLE", "Check the configured endpoint and network. Do not replay an uncertain mutation."
	}
	return "UPSTREAM_ERROR", "Inspect the returned details and the provider schema before deciding whether to retry."
}

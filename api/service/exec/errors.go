package exec

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	// ErrImageRequired is returned when Docker image is not specified
	ErrImageRequired = errors.New("docker image is required")
)

// Error implements apierror.Error for executor errors
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

// NewUnsupportedEntryKindError creates an error for unsupported entry kinds
func NewUnsupportedEntryKindError(kind registry.Kind) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("unsupported entry kind: %s", kind),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

// NewExecutorAlreadyExistsError creates an error when executor already exists
func NewExecutorAlreadyExistsError(id string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   fmt.Sprintf("executor %s already exists", id),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"executor_id": id}),
	}
}

// NewExecutorNotFoundError creates an error when executor is not found
func NewExecutorNotFoundError(id string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   fmt.Sprintf("executor %s not found", id),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"executor_id": id}),
	}
}

// NewConfigDecodeError creates an error for configuration decode failures
func NewConfigDecodeError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("failed to decode configuration: %v", err),
		retryable: apierror.False,
		cause:     err,
	}
}

// NewExecutorCreateError creates an error for executor creation failures
func NewExecutorCreateError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   fmt.Sprintf("failed to create executor: %v", err),
		retryable: apierror.True,
		cause:     err,
	}
}

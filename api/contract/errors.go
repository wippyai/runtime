package contract

import (
	"errors"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors for contract operations.
var (
	ErrInstantiatorNotFound = errors.New("contract instantiator not found in context")
	ErrInstanceNil          = errors.New("contract instance is nil")
	ErrNodeNotFound         = errors.New("relay node not found")
	ErrPIDNotFound          = errors.New("process PID not found in context")
)

// Error implements apierror.Error for contract errors.
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

// NewContractLoadError creates an error when loading a contract fails.
func NewContractLoadError(contractID registry.ID, err error) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "failed to load contract '" + contractID.String() + "': " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"contract_id": contractID.String(), "cause": err.Error()}),
		cause:     err,
	}
}

// NewMethodNotBoundError creates an error when a method is not bound.
func NewMethodNotBoundError(method string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "method '" + method + "' not bound",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"method": method}),
	}
}

// NewMissingContextKeysError creates an error when required context keys are missing.
func NewMissingContextKeysError(keys []string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "missing required context keys: [" + strings.Join(keys, ", ") + "]",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"missing_keys": keys}),
	}
}

// NewSubscriberError creates an error for subscriber creation failures.
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewContractNotFoundError creates an error when contract definition is not found.
func NewContractNotFoundError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "contract definition '" + id.String() + "' not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"contract_id": id.String()}),
	}
}

// NewBindingNotFoundError creates an error when contract binding is not found.
func NewBindingNotFoundError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "contract binding '" + id.String() + "' not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
	}
}

// NewNoDefaultBindingError creates an error when no default binding exists for a contract.
func NewNoDefaultBindingError(contractID registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no default binding for contract '" + contractID.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"contract_id": contractID.String()}),
	}
}

// NewMethodNotFoundError creates an error when a method is not found in a contract.
func NewMethodNotFoundError(method string, contractID registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "method '" + method + "' not found in contract '" + contractID.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"method": method, "contract_id": contractID.String()}),
	}
}

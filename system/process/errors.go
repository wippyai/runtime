package process

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error implements apierror.Error for process errors ;; todo: use erros from api?
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }

// NewFactoryNotFoundError creates an error for missing factory
func NewFactoryNotFoundError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no factory registered for: " + id.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"factory_id": id.String()}),
	}
}

// NewInvalidFactoryEntryError creates an error for invalid factory entry
func NewInvalidFactoryEntryError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "invalid factory entry for: " + id.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"factory_id": id.String()}),
	}
}

// NewProcessCreateError creates an error for process creation failures
func NewProcessCreateError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create process: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
	}
}

// NewHostNotFoundError creates an error for missing host
func NewHostNotFoundError(hostID string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "host not found: " + hostID,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"host_id": hostID}),
	}
}

// NewInvalidHostError creates an error for host that doesn't implement process.Host
func NewInvalidHostError(hostID string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "host " + hostID + " does not implement process.Host",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"host_id": hostID}),
	}
}

// NewSubscriberError creates an error for event subscriber failures
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
	}
}

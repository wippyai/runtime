package relay

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for relay operations.
var (
	ErrAlreadyAttached = &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "receiver already attached",
		retryable: apierror.False,
	}

	ErrHostNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "host not found",
		retryable: apierror.False,
	}

	ErrHostAlreadyExists = &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "host already exists",
		retryable: apierror.False,
	}

	ErrInvalidPIDFormat = &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid pid format",
		retryable: apierror.False,
	}

	ErrNilPackage = &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot send nil package",
		retryable: apierror.False,
	}

	ErrEmptyNodeID = &Error{
		kind:      apierror.KindInvalid,
		message:   "nodeID cannot be empty",
		retryable: apierror.False,
	}
)

// Error represents a relay error with metadata.
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

// WithCause returns a new error with the given cause.
func (e *Error) WithCause(cause error) *Error {
	return &Error{
		kind:      e.kind,
		message:   e.message,
		retryable: e.retryable,
		details:   e.details,
		cause:     cause,
	}
}

// WithDetails returns a new error with the given details.
func (e *Error) WithDetails(details attrs.Attributes) *Error {
	return &Error{
		kind:      e.kind,
		message:   e.message,
		retryable: e.retryable,
		details:   details,
		cause:     e.cause,
	}
}

// WithMessage returns a new error with a custom message.
func (e *Error) WithMessage(msg string) *Error {
	return &Error{
		kind:      e.kind,
		message:   msg,
		retryable: e.retryable,
		details:   e.details,
		cause:     e.cause,
	}
}

// NewHostExistsError creates an error when host already exists.
func NewHostExistsError(hostID HostID, nodeID NodeID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "host " + hostID + " already exists in node " + nodeID,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}),
	}
}

// NewHostNotFoundError creates an error when host is not found.
func NewHostNotFoundError(hostID HostID, nodeID NodeID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "host " + hostID + " not found in node " + nodeID,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}),
	}
}

// NewInvalidHostTypeError creates an error when host has invalid type.
func NewInvalidHostTypeError(hostID HostID, nodeID NodeID) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "host " + hostID + " in node " + nodeID + " has invalid type",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}),
	}
}

// NewExternalNodeError creates an error when trying to route to external node.
func NewExternalNodeError(nodeID NodeID) *Error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   "cannot route to external node " + nodeID,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
	}
}

// NewNodeNotFoundError creates an error when node is not found.
func NewNodeNotFoundError(nodeID NodeID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "cannot route to node " + nodeID + ": not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
	}
}

// NewHostNotAttachableError creates an error when host doesn't support attachment.
func NewHostNotAttachableError(hostID HostID) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "host " + hostID + " does not support attachment",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"host_id": hostID}),
	}
}

// NewPeerExistsError creates an error when peer node already exists.
func NewPeerExistsError(nodeID NodeID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "peer node already registered: " + nodeID,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
	}
}

// NewPeerConflictError creates an error when peer node conflicts with local node.
func NewPeerConflictError(nodeID NodeID) *Error {
	return &Error{
		kind:      apierror.KindConflict,
		message:   "peer nodeID conflicts with local node: " + nodeID,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
	}
}

// NewSubscriberError creates an error for event subscriber failures.
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewAlreadyAttachedError creates an error when a receiver is already attached.
func NewAlreadyAttachedError(pid PID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "already attached: pid=" + pid.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"pid": pid.String(), "host": pid.Host, "uniq_id": pid.UniqID}),
		cause:     ErrAlreadyAttached,
	}
}

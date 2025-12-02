package relay

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	api "github.com/wippyai/runtime/api/relay"
)

// Error implements apierror.Error for relay errors
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

// Sentinel errors
var (
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

// NewHostExistsError creates an error when host already exists
func NewHostExistsError(hostID api.HostID, nodeID api.NodeID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "host " + string(hostID) + " already exists in node " + string(nodeID),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"host_id": string(hostID), "node_id": string(nodeID)}),
	}
}

// NewHostNotFoundError creates an error when host is not found
func NewHostNotFoundError(hostID api.HostID, nodeID api.NodeID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "host " + string(hostID) + " not found in node " + string(nodeID),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"host_id": string(hostID), "node_id": string(nodeID)}),
	}
}

// NewInvalidHostTypeError creates an error when host has invalid type
func NewInvalidHostTypeError(hostID api.HostID, nodeID api.NodeID) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "host " + string(hostID) + " in node " + string(nodeID) + " has invalid type",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"host_id": string(hostID), "node_id": string(nodeID)}),
	}
}

// NewExternalNodeError creates an error when trying to route to external node
func NewExternalNodeError(nodeID api.NodeID) *Error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   "cannot route to external node " + string(nodeID),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": string(nodeID)}),
	}
}

// NewNodeNotFoundError creates an error when node is not found
func NewNodeNotFoundError(nodeID api.NodeID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "cannot route to node " + string(nodeID) + ": not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": string(nodeID)}),
	}
}

// NewHostNotAttachableError creates an error when host doesn't support attachment
func NewHostNotAttachableError(hostID api.HostID) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "host " + string(hostID) + " does not support attachment",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"host_id": string(hostID)}),
	}
}

// NewVirtualNodeExistsError creates an error when virtual node already exists
func NewVirtualNodeExistsError(nodeID api.NodeID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "virtual node already registered: " + string(nodeID),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": string(nodeID)}),
	}
}

// NewVirtualNodeConflictError creates an error when virtual node conflicts with local node
func NewVirtualNodeConflictError(nodeID api.NodeID) *Error {
	return &Error{
		kind:      apierror.KindConflict,
		message:   "nodeID conflicts with local node: " + string(nodeID),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"node_id": string(nodeID)}),
	}
}

// NewSubscriberError creates an error for event subscriber failures
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewAlreadyAttachedError creates an error when a receiver is already attached
func NewAlreadyAttachedError(pid api.PID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "already attached: pid=" + pid.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"pid": pid.String(), "host": pid.Host, "uniq_id": pid.UniqID}),
		cause:     api.ErrAlreadyAttached,
	}
}

package relay

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
)

var (
	ErrNilPackage      = apierror.New(apierror.Invalid, "cannot send nil package").WithRetryable(apierror.False)
	ErrAlreadyAttached = apierror.New(apierror.AlreadyExists, "receiver already attached").WithRetryable(apierror.False)
)

// NewInvalidHostTypeError creates an error when host has invalid type.
func NewInvalidHostTypeError(hostID pid.HostID, nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"host "+hostID+" in node "+nodeID+" has invalid type",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}),
		nil,
	)
}

// NewSubscriberError creates an error for event subscriber failures.
func NewSubscriberError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to create subscriber: "+err.Error(),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewNilPackageError creates an error when a nil package is passed to Send.
func NewNilPackageError() apierror.Error {
	return apierror.E(
		apierror.Invalid,
		"cannot send nil package",
		apierror.False,
		nil,
		ErrNilPackage,
	)
}

// NewHostExistsError creates an error when host already exists.
func NewHostExistsError(hostID pid.HostID, nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.AlreadyExists,
		"host "+hostID+" already exists in node "+nodeID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}),
		nil,
	)
}

// NewHostNotFoundError creates an error when host is not found.
func NewHostNotFoundError(hostID pid.HostID, nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"host "+hostID+" not found in node "+nodeID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}),
		nil,
	)
}

// NewExternalNodeError creates an error when trying to route to external node.
func NewExternalNodeError(nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.Unavailable,
		"cannot route to external node "+nodeID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
		nil,
	)
}

// NewNodeNotFoundError creates an error when node is not found.
func NewNodeNotFoundError(nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"cannot route to node "+nodeID+": not found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
		nil,
	)
}

// NewHostNotAttachableError creates an error when host doesn't support attachment.
func NewHostNotAttachableError(hostID pid.HostID) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		"host "+hostID+" does not support attachment",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"host_id": hostID}),
		nil,
	)
}

// NewPeerExistsError creates an error when peer node already exists.
func NewPeerExistsError(nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.AlreadyExists,
		"peer node already registered: "+nodeID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
		nil,
	)
}

// NewPeerConflictError creates an error when peer node conflicts with local node.
func NewPeerConflictError(nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.Conflict,
		"peer nodeID conflicts with local node: "+nodeID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
		nil,
	)
}

// NewAlreadyAttachedError creates an error when a receiver is already attached.
func NewAlreadyAttachedError(p pid.PID) apierror.Error {
	return apierror.E(
		apierror.AlreadyExists,
		"already attached: pid="+p.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"pid": p.String(), "host": p.Host, "uniq_id": p.UniqID}),
		ErrAlreadyAttached,
	)
}

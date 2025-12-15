package relay

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
)

// Sentinel errors for relay operations.
var (
	ErrAlreadyAttached   = apierror.New(apierror.KindAlreadyExists, "receiver already attached").WithRetryable(apierror.False)
	ErrHostNotFound      = apierror.New(apierror.KindNotFound, "host not found").WithRetryable(apierror.False)
	ErrHostAlreadyExists = apierror.New(apierror.KindAlreadyExists, "host already exists").WithRetryable(apierror.False)
	ErrEmptyNodeID       = apierror.New(apierror.KindInvalid, "nodeID cannot be empty").WithRetryable(apierror.False)
)

// NewHostExistsError creates an error when host already exists.
func NewHostExistsError(hostID pid.HostID, nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.KindAlreadyExists,
		"host "+hostID+" already exists in node "+nodeID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}),
		nil,
	)
}

// NewHostNotFoundError creates an error when host is not found.
func NewHostNotFoundError(hostID pid.HostID, nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"host "+hostID+" not found in node "+nodeID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}),
		nil,
	)
}

// NewExternalNodeError creates an error when trying to route to external node.
func NewExternalNodeError(nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.KindUnavailable,
		"cannot route to external node "+nodeID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
		nil,
	)
}

// NewNodeNotFoundError creates an error when node is not found.
func NewNodeNotFoundError(nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"cannot route to node "+nodeID+": not found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
		nil,
	)
}

// NewHostNotAttachableError creates an error when host doesn't support attachment.
func NewHostNotAttachableError(hostID pid.HostID) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"host "+hostID+" does not support attachment",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"host_id": hostID}),
		nil,
	)
}

// NewPeerExistsError creates an error when peer node already exists.
func NewPeerExistsError(nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.KindAlreadyExists,
		"peer node already registered: "+nodeID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
		nil,
	)
}

// NewPeerConflictError creates an error when peer node conflicts with local node.
func NewPeerConflictError(nodeID pid.NodeID) apierror.Error {
	return apierror.E(
		apierror.KindConflict,
		"peer nodeID conflicts with local node: "+nodeID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"node_id": nodeID}),
		nil,
	)
}

// NewAlreadyAttachedError creates an error when a receiver is already attached.
func NewAlreadyAttachedError(p pid.PID) apierror.Error {
	return apierror.E(
		apierror.KindAlreadyExists,
		"already attached: pid="+p.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"pid": p.String(), "host": p.Host, "uniq_id": p.UniqID}),
		ErrAlreadyAttached,
	)
}

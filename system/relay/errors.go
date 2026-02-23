// SPDX-License-Identifier: MPL-2.0

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
	return apierror.New(apierror.Internal, "invalid host type").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}))
}

// NewSubscriberError creates an error for event subscriber failures.
func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewNilPackageError creates an error when a nil package is passed to Send.
func NewNilPackageError() apierror.Error {
	return apierror.New(apierror.Invalid, "cannot send nil package").
		WithRetryable(apierror.False).
		WithCause(ErrNilPackage)
}

// NewHostExistsError creates an error when host already exists.
func NewHostExistsError(hostID pid.HostID, nodeID pid.NodeID) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "host already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}))
}

// NewHostNotFoundError creates an error when host is not found.
func NewHostNotFoundError(hostID pid.HostID, nodeID pid.NodeID) apierror.Error {
	return apierror.New(apierror.NotFound, "host not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"host_id": hostID, "node_id": nodeID}))
}

// NewExternalNodeError creates an error when trying to route to external node.
func NewExternalNodeError(nodeID pid.NodeID) apierror.Error {
	return apierror.New(apierror.Unavailable, "cannot route to external node").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node_id": nodeID}))
}

// NewNodeNotFoundError creates an error when node is not found.
func NewNodeNotFoundError(nodeID pid.NodeID) apierror.Error {
	return apierror.New(apierror.NotFound, "node not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node_id": nodeID}))
}

// NewHostNotAttachableError creates an error when host doesn't support attachment.
func NewHostNotAttachableError(hostID pid.HostID) apierror.Error {
	return apierror.New(apierror.Invalid, "host does not support attachment").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"host_id": hostID}))
}

// NewPeerExistsError creates an error when peer node already exists.
func NewPeerExistsError(nodeID pid.NodeID) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "peer node already registered").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node_id": nodeID}))
}

// NewPeerConflictError creates an error when peer node conflicts with local node.
func NewPeerConflictError(nodeID pid.NodeID) apierror.Error {
	return apierror.New(apierror.Conflict, "peer node conflicts with local node").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"node_id": nodeID}))
}

// NewAlreadyAttachedError creates an error when a receiver is already attached.
func NewAlreadyAttachedError(p pid.PID) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "receiver already attached").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"pid": p.String(), "host": p.Host, "uniq_id": p.UniqID})).
		WithCause(ErrAlreadyAttached)
}

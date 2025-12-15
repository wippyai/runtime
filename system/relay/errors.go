package relay

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
)

// Sentinel errors for relay operations.
var (
	ErrNilPackage = apierror.New(apierror.Invalid, "cannot send nil package").WithRetryable(apierror.False)
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

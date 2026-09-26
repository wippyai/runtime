// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDataSizeExceedsMax = apierror.New(apierror.Invalid, "data size exceeds maximum").WithRetryable(apierror.False)

	ErrAdvertisedSizeExceedsMax = apierror.New(apierror.Invalid, "advertised size exceeds maximum").WithRetryable(apierror.False)

	ErrFailedToAppendCACerts = apierror.New(apierror.Invalid, "failed to append ca certs").WithRetryable(apierror.False)
)

func newNodeAdmissionError(nodeID string, cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "node is not available for message admission: "+nodeID).
		WithRetryable(apierror.True).
		WithDetails(attrs.Bag{"node_id": nodeID}).
		WithCause(cause)
}

func NewSetDeadlineError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to set deadline").WithCause(err).WithRetryable(apierror.False)
}

func NewWriteNodeIDError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to write node ID").WithCause(err).WithRetryable(apierror.False)
}

func NewReadNodeIDError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read node ID").WithCause(err).WithRetryable(apierror.False)
}

func NewNodeIDMismatchError(expected, actual string) apierror.Error {
	return apierror.New(apierror.Invalid, "node ID mismatch").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"expected": expected, "actual": actual})
}

// newPeerNotAuthorizedError reports a peer that proved its identity but is
// not admitted to this node's mesh.
func newPeerNotAuthorizedError(nodeID string) apierror.Error {
	return apierror.New(apierror.PermissionDenied, "internode peer is not authorized").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"node_id": nodeID})
}

func NewEncodePayloadError(index int, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to encode payload").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"payload_index": index}).
		WithCause(err)
}

func NewMsgpackEncodeError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to msgpack encode package").WithCause(err).WithRetryable(apierror.False)
}

func NewMsgpackDecodeError(err error, isEmptyOrIncomplete bool) apierror.Error {
	msg := "failed to msgpack decode package"
	if isEmptyOrIncomplete {
		msg = "failed to msgpack decode package: buffer is empty or incomplete"
	}
	return apierror.New(apierror.Invalid, msg).WithCause(err).WithRetryable(apierror.False)
}

func NewLoadTLSError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to load TLS configuration").WithCause(err).WithRetryable(apierror.False)
}

func NewStartListenerError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start listener").WithCause(err).WithRetryable(apierror.False)
}

func NewLoadKeyPairError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "could not load key pair").WithCause(err).WithRetryable(apierror.False)
}

func NewReadCACertError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "could not read ca certificate").WithCause(err).WithRetryable(apierror.False)
}

func NewMessageTooLargeError(size int) apierror.Error {
	return apierror.New(apierror.Invalid, "message too large").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"size": size})
}

func NewStartConnectionManagerError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start connection manager").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewSubscribeMembershipError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to subscribe to membership events").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewEncodePackageError(targetNode string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to encode package for node "+targetNode).
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"target_node": targetNode}).
		WithCause(cause)
}

func NewMessageSizeExceedsMaxError(size, maxSize int) apierror.Error {
	return apierror.New(apierror.Invalid, "message size exceeds maximum").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"size": size, "max_size": maxSize})
}

func newDecodePayloadError(index int, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode payload").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"payload_index": index}).
		WithCause(cause)
}

func newRegisterExtensionError(tag uint64, goType string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to register msgpack extension").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"tag": tag, "type": goType}).
		WithCause(cause)
}

// newWireTypeError reports a value whose Go type has no wire form in the
// position it occupies.
func newWireTypeError(position string, value any) apierror.Error {
	return apierror.New(apierror.Invalid, "unexpected wire value type").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"position": position, "type": fmt.Sprintf("%T", value)})
}

func newEmptyErrorChainError() apierror.Error {
	return apierror.New(apierror.Invalid, "error chain has no entries").WithRetryable(apierror.False)
}

var (
	// errSessionEnded stops a connection whose session ended while it ran.
	errSessionEnded = apierror.New(apierror.Unavailable, "internode peer session ended").WithRetryable(apierror.True)

	// errPeerSessionReset reports that the peer ended its session for this
	// node; the connection closes and the session starts afresh.
	errPeerSessionReset = apierror.New(apierror.Unavailable, "internode peer ended its session").WithRetryable(apierror.True)

	errZeroIncarnation = apierror.New(apierror.Invalid, "internode incarnation must be nonzero").WithRetryable(apierror.False)
)

func newSequenceGapError(expected, got uint64) apierror.Error {
	return apierror.New(apierror.Invalid, "internode frame sequence gap").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"expected": expected, "got": got})
}

func newAckRangeError(ack, acked, sendNext uint64) apierror.Error {
	return apierror.New(apierror.Invalid, "internode ack outside the sent range").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"ack": ack, "acked": acked, "send_next": sendNext})
}

func newResumeViewError(view, id uint64) apierror.Error {
	return apierror.New(apierror.Invalid, "internode peer resumed an unknown session of this node").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"peer_view": view, "session": id})
}

func newFrameError(reason string, class Class) apierror.Error {
	return apierror.New(apierror.Invalid, "internode frame violates the session protocol: "+reason).
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"class": uint8(class)})
}

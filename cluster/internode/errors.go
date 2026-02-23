// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDataSizeExceedsMax = apierror.New(apierror.Invalid, "data size exceeds maximum").WithRetryable(apierror.False)

	ErrAdvertisedSizeExceedsMax = apierror.New(apierror.Invalid, "advertised size exceeds maximum").WithRetryable(apierror.False)

	ErrFailedToAppendCACerts = apierror.New(apierror.Invalid, "failed to append ca certs").WithRetryable(apierror.False)
)

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

func NewRegisterPIDExtensionError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to register pid.PID extension").WithCause(cause).WithRetryable(apierror.False)
}

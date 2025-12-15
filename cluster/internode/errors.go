package internode

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDataSizeExceedsMax = apierror.New(apierror.KindInvalid, "data size exceeds maximum").WithRetryable(apierror.False)

	ErrAdvertisedSizeExceedsMax = apierror.New(apierror.KindInvalid, "advertised size exceeds maximum").WithRetryable(apierror.False)

	ErrFailedToAppendCACerts = apierror.New(apierror.KindInvalid, "failed to append ca certs").WithRetryable(apierror.False)
)

func NewSetDeadlineError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to set deadline").WithCause(err).WithRetryable(apierror.False)
}

func NewWriteNodeIDError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to write node ID").WithCause(err).WithRetryable(apierror.False)
}

func NewReadNodeIDError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to read node ID").WithCause(err).WithRetryable(apierror.False)
}

func NewNodeIDMismatchError(expected, actual string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "node ID mismatch").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"expected": expected, "actual": actual})
}

func NewEncodePayloadError(index int, err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to encode payload").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"payload_index": index}).
		WithCause(err)
}

func NewMsgpackEncodeError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to msgpack encode package").WithCause(err).WithRetryable(apierror.False)
}

func NewMsgpackDecodeError(err error, isEmptyOrIncomplete bool) apierror.Error {
	msg := "failed to msgpack decode package"
	if isEmptyOrIncomplete {
		msg = "failed to msgpack decode package: buffer is empty or incomplete"
	}
	return apierror.New(apierror.KindInvalid, msg).WithCause(err).WithRetryable(apierror.False)
}

func NewLoadTLSError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to load TLS configuration").WithCause(err).WithRetryable(apierror.False)
}

func NewStartListenerError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start listener").WithCause(err).WithRetryable(apierror.False)
}

func NewLoadKeyPairError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "could not load key pair").WithCause(err).WithRetryable(apierror.False)
}

func NewReadCACertError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "could not read ca certificate").WithCause(err).WithRetryable(apierror.False)
}

func NewMessageTooLargeError(size int) apierror.Error {
	return apierror.New(apierror.KindInvalid, "message too large").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"size": size})
}

func NewStartConnectionManagerError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start connection manager").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewSubscribeMembershipError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to subscribe to membership events").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewEncodePackageError(targetNode string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to encode package for node "+targetNode).
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"target_node": targetNode}).
		WithCause(cause)
}

func NewMessageSizeExceedsMaxError(size, maxSize int) apierror.Error {
	return apierror.New(apierror.KindInvalid, "message size exceeds maximum").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"size": size, "max_size": maxSize})
}

func NewRegisterPIDExtensionError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to register pid.PID extension").WithCause(cause).WithRetryable(apierror.False)
}

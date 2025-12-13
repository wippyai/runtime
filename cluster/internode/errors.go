package internode

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

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

var (
	ErrDataSizeExceedsMax = &Error{
		kind:      apierror.KindInvalid,
		message:   "data size exceeds maximum",
		retryable: apierror.False,
	}

	ErrAdvertisedSizeExceedsMax = &Error{
		kind:      apierror.KindInvalid,
		message:   "advertised size exceeds maximum",
		retryable: apierror.False,
	}
)

func NewSetDeadlineError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to set deadline",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewWriteNodeIDError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to write node ID",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewReadNodeIDError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to read node ID",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewNodeIDMismatchError(expected, actual string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "node ID mismatch",
		retryable: apierror.False,
		details: attrs.Bag{
			"expected": expected,
			"actual":   actual,
		},
	}
}

func NewEncodePayloadError(index int, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to encode payload",
		retryable: apierror.False,
		details: attrs.Bag{
			"payload_index": index,
		},
		cause: err,
	}
}

func NewMsgpackEncodeError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to msgpack encode package",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewMsgpackDecodeError(err error, isEmptyOrIncomplete bool) *Error {
	msg := "failed to msgpack decode package"
	if isEmptyOrIncomplete {
		msg = "failed to msgpack decode package: buffer is empty or incomplete"
	}
	return &Error{
		kind:      apierror.KindInvalid,
		message:   msg,
		retryable: apierror.False,
		cause:     err,
	}
}

func NewLoadTLSError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to load TLS configuration",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewStartListenerError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to start listener",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewLoadKeyPairError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "could not load key pair",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewReadCACertError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "could not read ca certificate",
		retryable: apierror.False,
		cause:     err,
	}
}

var (
	ErrFailedToAppendCACerts = &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to append ca certs",
		retryable: apierror.False,
	}
)

func NewMessageTooLargeError(size int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "message too large",
		retryable: apierror.False,
		details: attrs.Bag{
			"size": size,
		},
	}
}

func NewStartConnectionManagerError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to start connection manager",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewSubscribeMembershipError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to subscribe to membership events",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewEncodePackageError(targetNode string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to encode package for node " + targetNode,
		retryable: apierror.False,
		details:   attrs.Bag{"target_node": targetNode},
		cause:     cause,
	}
}

func NewMessageSizeExceedsMaxError(size, maxSize int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "message size exceeds maximum",
		retryable: apierror.False,
		details:   attrs.Bag{"size": size, "max_size": maxSize},
	}
}

func NewRegisterPIDExtensionError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to register pid.PID extension",
		retryable: apierror.False,
		cause:     cause,
	}
}

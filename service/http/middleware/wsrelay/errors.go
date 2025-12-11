package wsrelay

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
	ErrHostRequired         = &Error{kind: apierror.KindInvalid, message: "host is required"}
	ErrNodeRequired         = &Error{kind: apierror.KindInvalid, message: "node is required"}
	ErrTranscoderRequired   = &Error{kind: apierror.KindInvalid, message: "transcoder is required"}
	ErrFrameContextNotFound = &Error{kind: apierror.KindInternal, message: "FrameContext not found"}
	ErrServerHostNotFound   = &Error{kind: apierror.KindInternal, message: "HTTP server host not found in context"}
	ErrNodeNotFound         = &Error{kind: apierror.KindInternal, message: "Node not found in context"}
	ErrTranscoderNotFound   = &Error{kind: apierror.KindInternal, message: "Transcoder not found in context"}
	ErrTopologyNotFound     = &Error{kind: apierror.KindInternal, message: "Topology not found in context"}
	ErrServerIDNotFound     = &Error{kind: apierror.KindInternal, message: "Server ID not found in context"}
	ErrInvalidServerID      = &Error{kind: apierror.KindInvalid, message: "invalid server ID in context"}
	ErrHostNotAttachable    = &Error{kind: apierror.KindInvalid, message: "server host does not implement AttachableHost"}
	ErrExpectedBytesPayload = &Error{kind: apierror.KindInvalid, message: "expected bytes payload but got different type"}
)

func NewHostAttachmentError(hostType string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "host does not support attachment: " + hostType,
	}
}

func NewAttachToRelayError(err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to attach to relay",
		cause:   err,
	}
}

func NewTranscodeError(err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to transcode payload to JSON",
		cause:   err,
	}
}

func NewMarshalError(what string, err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "error marshaling " + what,
		cause:   err,
	}
}

func NewWebSocketWriteError(err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "error writing to WebSocket",
		cause:   err,
	}
}

func NewMarshalJoinInfoError(err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to marshal join info",
		cause:   err,
	}
}

func NewMarshalLeaveInfoError(err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to marshal leave info",
		cause:   err,
	}
}

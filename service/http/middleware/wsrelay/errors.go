package wsrelay

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrHostRequired         = apierror.New(apierror.KindInvalid, "host is required").WithRetryable(apierror.False)
	ErrNodeRequired         = apierror.New(apierror.KindInvalid, "node is required").WithRetryable(apierror.False)
	ErrTranscoderRequired   = apierror.New(apierror.KindInvalid, "transcoder is required").WithRetryable(apierror.False)
	ErrFrameContextNotFound = apierror.New(apierror.KindInternal, "FrameContext not found").WithRetryable(apierror.False)
	ErrServerHostNotFound   = apierror.New(apierror.KindInternal, "HTTP server host not found in context").WithRetryable(apierror.False)
	ErrServerIDNotFound     = apierror.New(apierror.KindInternal, "Server ID not found in context").WithRetryable(apierror.False)
	ErrInvalidServerID      = apierror.New(apierror.KindInvalid, "invalid server ID in context").WithRetryable(apierror.False)
	ErrHostNotAttachable    = apierror.New(apierror.KindInvalid, "server host does not implement AttachableHost").WithRetryable(apierror.False)
	ErrExpectedBytesPayload = apierror.New(apierror.KindInvalid, "expected bytes payload but got different type").WithRetryable(apierror.False)
)

func NewAttachToRelayError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to attach to relay").WithCause(err)
}

func NewTranscodeError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to transcode payload to JSON").WithCause(err)
}

func NewMarshalError(what string, err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "error marshaling "+what).WithCause(err)
}

func NewWebSocketWriteError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "error writing to WebSocket").WithCause(err)
}

func NewMarshalJoinInfoError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to marshal join info").WithCause(err)
}

func NewMarshalLeaveInfoError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to marshal leave info").WithCause(err)
}

// SPDX-License-Identifier: MPL-2.0

package wsrelay

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrHostRequired         = apierror.New(apierror.Invalid, "host is required").WithRetryable(apierror.False)
	ErrNodeRequired         = apierror.New(apierror.Invalid, "node is required").WithRetryable(apierror.False)
	ErrTranscoderRequired   = apierror.New(apierror.Invalid, "transcoder is required").WithRetryable(apierror.False)
	ErrFrameContextNotFound = apierror.New(apierror.Internal, "FrameContext not found").WithRetryable(apierror.False)
	ErrServerHostNotFound   = apierror.New(apierror.Internal, "HTTP server host not found in context").WithRetryable(apierror.False)
	ErrServerIDNotFound     = apierror.New(apierror.Internal, "Server ID not found in context").WithRetryable(apierror.False)
	ErrInvalidServerID      = apierror.New(apierror.Invalid, "invalid server ID in context").WithRetryable(apierror.False)
	ErrHostNotAttachable    = apierror.New(apierror.Invalid, "server host does not implement AttachableHost").WithRetryable(apierror.False)
	ErrExpectedBytesPayload = apierror.New(apierror.Invalid, "expected bytes payload but got different type").WithRetryable(apierror.False)
)

func NewAttachToRelayError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to attach to relay").
		WithRetryable(apierror.False).
		WithCause(err)
}

func NewTranscodeError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to transcode payload to JSON").
		WithRetryable(apierror.False).
		WithCause(err)
}

func NewMarshalError(what string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "error marshaling "+what).
		WithRetryable(apierror.False).
		WithCause(err)
}

func NewWebSocketWriteError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "error writing to WebSocket").
		WithRetryable(apierror.False).
		WithCause(err)
}

func NewMarshalJoinInfoError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to marshal join info").
		WithRetryable(apierror.False).
		WithCause(err)
}

func NewMarshalLeaveInfoError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to marshal leave info").
		WithRetryable(apierror.False).
		WithCause(err)
}

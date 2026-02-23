// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrHostRequired         = apierror.New(apierror.Invalid, "host is required").WithRetryable(apierror.False)
	ErrNodeRequired         = apierror.New(apierror.Invalid, "node is required").WithRetryable(apierror.False)
	ErrTopologyRequired     = apierror.New(apierror.Invalid, "topology is required").WithRetryable(apierror.False)
	ErrTranscoderRequired   = apierror.New(apierror.Invalid, "transcoder is required").WithRetryable(apierror.False)
	ErrPIDGeneratorRequired = apierror.New(apierror.Invalid, "pid generator is required").WithRetryable(apierror.False)

	ErrFrameContextNotFound = apierror.New(apierror.Internal, "frame context not found").WithRetryable(apierror.False)
	ErrServerHostNotFound   = apierror.New(apierror.Internal, "http server host not found in context").WithRetryable(apierror.False)
	ErrServerIDNotFound     = apierror.New(apierror.Internal, "server ID not found in context").WithRetryable(apierror.False)
	ErrInvalidServerID      = apierror.New(apierror.Invalid, "invalid server ID in context").WithRetryable(apierror.False)
	ErrHostNotAttachable    = apierror.New(apierror.Invalid, "server host does not implement AttachableReceiver").WithRetryable(apierror.False)

	ErrSSEFlusherUnavailable = apierror.New(apierror.Internal, "response writer does not support flushing").WithRetryable(apierror.False)
	ErrExpectedBytesPayload  = apierror.New(apierror.Invalid, "expected bytes payload but got different type").WithRetryable(apierror.False)
	ErrOriginNotAllowed      = apierror.New(apierror.PermissionDenied, "origin is not allowed").WithRetryable(apierror.False)
)

func newParseDurationError(field, value string, err error) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid %s duration: %q", field, value)).
		WithRetryable(apierror.False).
		WithCause(err)
}

func newNegativeDurationError(field, value string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("%s duration must be non-negative: %q", field, value)).
		WithRetryable(apierror.False)
}

func newTargetPIDError(value string, err error) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid target PID: %q", value)).
		WithRetryable(apierror.False).
		WithCause(err)
}

func newAttachError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to attach relay session").
		WithRetryable(apierror.False).
		WithCause(err)
}

func newMarshalError(what string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to marshal "+what).
		WithRetryable(apierror.False).
		WithCause(err)
}

func newTranscodeError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to transcode payload to json").
		WithRetryable(apierror.False).
		WithCause(err)
}

// SPDX-License-Identifier: MPL-2.0

package entry

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrConfigurationDataRequired = apierror.New(apierror.Invalid, "configuration data is required").WithRetryable(apierror.False)

	ErrEmptyPath = apierror.New(apierror.Invalid, "path cannot be empty").WithRetryable(apierror.False)

	ErrCannotReplaceEntireDataField = apierror.New(apierror.Invalid, "cannot replace entire data field").WithRetryable(apierror.False)

	ErrCannotReplaceEntireMetaField = apierror.New(apierror.Invalid, "cannot replace entire meta field").WithRetryable(apierror.False)

	ErrEmptyPathSegments = apierror.New(apierror.Invalid, "path segments cannot be empty").WithRetryable(apierror.False)
)

func NewUnmarshalConfigError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Invalid, "failed to unmarshal config").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewInvalidConfigurationError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Invalid, "invalid configuration").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewInvalidTargetError(target string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid target: "+target).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"target": target}))
}

var (
	ErrCannotAppendToEntireDataField = apierror.New(apierror.Invalid, "cannot append to entire data field").WithRetryable(apierror.False)

	ErrCannotAppendToEntireMetaField = apierror.New(apierror.Invalid, "cannot append to entire meta field").WithRetryable(apierror.False)

	ErrCannotDeleteEntireDataField = apierror.New(apierror.Invalid, "cannot delete entire data field").WithRetryable(apierror.False)

	ErrCannotDeleteEntireMetaField = apierror.New(apierror.Invalid, "cannot delete entire meta field").WithRetryable(apierror.False)
)

func NewTranscodeToGolangError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to transcode to golang").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewCannotAppendToNonArrayError(path string) apierror.Error {
	return apierror.New(apierror.Invalid, "cannot append to non-array at path: "+path).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}

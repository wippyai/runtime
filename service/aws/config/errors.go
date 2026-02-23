// SPDX-License-Identifier: MPL-2.0

package config

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewUnsupportedKindError(kind string) error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewConfigAlreadyExistsError(id string) error {
	return apierror.New(apierror.AlreadyExists, "config already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewDecodeConfigError(cause error) error {
	apiErr := apierror.New(apierror.Invalid, "failed to decode config").
		WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewCreateAWSConfigError(cause error) error {
	apiErr := apierror.New(apierror.Internal, "failed to create AWS config").
		WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewConfigNotFoundError(id string) error {
	return apierror.New(apierror.NotFound, "config not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

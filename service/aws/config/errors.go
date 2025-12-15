package config

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewUnsupportedKindError(kind string) error {
	return apierror.New(apierror.Invalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewConfigAlreadyExistsError(id string) error {
	return apierror.New(apierror.AlreadyExists, "config "+id+" already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewDecodeConfigError(cause error) error {
	return apierror.New(apierror.Invalid, "failed to decode config").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateAWSConfigError(cause error) error {
	return apierror.New(apierror.Internal, "failed to create AWS config").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewConfigNotFoundError(id string) error {
	return apierror.New(apierror.NotFound, "config "+id+" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

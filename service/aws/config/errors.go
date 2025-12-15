package config

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewUnsupportedKindError(kind string) error {
	return apierror.New(apierror.KindInvalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewConfigAlreadyExistsError(id string) error {
	return apierror.New(apierror.KindAlreadyExists, "config "+id+" already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewDecodeConfigError(cause error) error {
	return apierror.New(apierror.KindInvalid, "failed to decode config").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateAWSConfigError(cause error) error {
	return apierror.New(apierror.KindInternal, "failed to create AWS config").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewConfigNotFoundError(id string) error {
	return apierror.New(apierror.KindNotFound, "config "+id+" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

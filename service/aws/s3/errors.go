package s3

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewUnsupportedKindError(kind string) error {
	return apierror.New(apierror.KindInvalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewStorageAlreadyExistsError(id string) error {
	return apierror.New(apierror.KindAlreadyExists, "storage "+id+" already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewAddEntryError(cause error) error {
	return apierror.New(apierror.KindInternal, "failed to add entry").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewStorageNotFoundError(id string) error {
	return apierror.New(apierror.KindNotFound, "storage "+id+" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewUpdateEntryError(cause error) error {
	return apierror.New(apierror.KindInternal, "failed to update entry").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewDecodeConfigError(cause error) error {
	return apierror.New(apierror.KindInvalid, "failed to decode config").WithCause(cause).WithRetryable(apierror.False)
}

func NewAcquireResourceError(cause error) error {
	return apierror.New(apierror.KindInternal, "failed to acquire resource").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewGetConfigError(cause error) error {
	return apierror.New(apierror.KindInternal, "failed to get config").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewAWSConfigInvalidError() error {
	return apierror.New(apierror.KindInternal, "aws config resource is not a valid config").WithRetryable(apierror.False)
}

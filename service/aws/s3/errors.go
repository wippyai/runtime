package s3

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewUnsupportedKindError(kind string) error {
	return apierror.New(apierror.Invalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewStorageAlreadyExistsError(id string) error {
	return apierror.New(apierror.AlreadyExists, "storage "+id+" already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewAddEntryError(cause error) error {
	return apierror.New(apierror.Internal, "failed to add entry").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewStorageNotFoundError(id string) error {
	return apierror.New(apierror.NotFound, "storage "+id+" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewUpdateEntryError(cause error) error {
	return apierror.New(apierror.Internal, "failed to update entry").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewDecodeConfigError(cause error) error {
	return apierror.New(apierror.Invalid, "failed to decode config").WithCause(cause).WithRetryable(apierror.False)
}

func NewAcquireResourceError(cause error) error {
	return apierror.New(apierror.Internal, "failed to acquire resource").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewGetConfigError(cause error) error {
	return apierror.New(apierror.Internal, "failed to get config").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewAWSConfigInvalidError() error {
	return apierror.New(apierror.Internal, "aws config resource is not a valid config").WithRetryable(apierror.False)
}

package s3

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewUnsupportedKindError(kind string) error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewStorageAlreadyExistsError(id string) error {
	return apierror.New(apierror.AlreadyExists, "storage already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewAddEntryError(cause error) error {
	apiErr := apierror.New(apierror.Internal, "failed to add entry").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewStorageNotFoundError(id string) error {
	return apierror.New(apierror.NotFound, "storage not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewUpdateEntryError(cause error) error {
	apiErr := apierror.New(apierror.Internal, "failed to update entry").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewDecodeConfigError(cause error) error {
	apiErr := apierror.New(apierror.Invalid, "failed to decode config").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewAcquireResourceError(cause error) error {
	apiErr := apierror.New(apierror.Internal, "failed to acquire resource").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewGetConfigError(cause error) error {
	apiErr := apierror.New(apierror.Internal, "failed to get config").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewAWSConfigInvalidError() error {
	return apierror.New(apierror.Internal, "aws config resource is not a valid config").WithRetryable(apierror.False)
}

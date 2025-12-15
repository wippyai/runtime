package env

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber: "+err.Error()).
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewInvalidStorageTypeError(id string) apierror.Error {
	return apierror.New(apierror.Internal, "invalid storage type for "+id).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewCreateStorageError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create storage").WithCause(err).WithRetryable(apierror.False)
}

func NewRenameTempFileError(attempts int, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to rename temp file").
		WithRetryable(apierror.Unspecified).
		WithDetails(attrs.NewBagFrom(map[string]any{"attempts": attempts})).
		WithCause(err)
}

func NewRenameTempFileAfterRemoveError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to rename temp file after removing target").WithCause(err).WithRetryable(apierror.Unspecified)
}

package fs

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

func NewGetFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get filesystem").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewCreateFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create filesystem").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewCreateDirectoryError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create directory").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewOpenDirectoryError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to open directory").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewStatError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "stat failed").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewOpenError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "open failed").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewGetEmbeddedFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get embedded filesystem").WithCause(err).WithRetryable(apierror.Unspecified)
}

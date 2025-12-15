package fs

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create subscriber: "+err.Error()).
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewGetFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to get filesystem").WithCause(err).WithRetryable(apierror.Unknown)
}

func NewCreateFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create filesystem").WithCause(err).WithRetryable(apierror.Unknown)
}

func NewCreateDirectoryError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create directory").WithCause(err).WithRetryable(apierror.Unknown)
}

func NewOpenDirectoryError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to open directory").WithCause(err).WithRetryable(apierror.Unknown)
}

func NewStatError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "stat failed").WithCause(err).WithRetryable(apierror.Unknown)
}

func NewOpenError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "open failed").WithCause(err).WithRetryable(apierror.Unknown)
}

func NewGetEmbeddedFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to get embedded filesystem").WithCause(err).WithRetryable(apierror.Unknown)
}

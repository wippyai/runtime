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

func NewUnsupportedKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewVariableNotFoundError(name string) apierror.Error {
	return apierror.New(apierror.NotFound, "environment variable not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"variable": name}))
}

func NewStorageNotFoundError(storageID string) apierror.Error {
	return apierror.New(apierror.NotFound, "environment storage backend not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"storage": storageID}))
}

func NewInvalidVariableNameError(name string, reason string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid environment variable name: "+reason).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"variable": name, "reason": reason}))
}

func NewInvalidVariableError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid variable: "+err.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewVariableNameExistsError(name string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "variable name already exists: "+name).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}

func NewDecodeConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode configuration").WithCause(err).WithRetryable(apierror.False)
}

func NewInvalidConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid configuration").WithCause(err).WithRetryable(apierror.False)
}

func NewStorageNotExistsError(storageID string) apierror.Error {
	return apierror.New(apierror.NotFound, "storage does not exist").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"storage_id": storageID}))
}

func NewDecodeVariableError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode variable").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

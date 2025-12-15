package env

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrVariableNotFound = apierror.New(apierror.KindNotFound, "environment variable not found").WithRetryable(apierror.False)

	ErrStorageNotFound = apierror.New(apierror.KindNotFound, "environment storage backend not found").WithRetryable(apierror.False)

	ErrVariableReadOnly = apierror.New(apierror.KindPermissionDenied, "environment variable is read-only").WithRetryable(apierror.False)

	ErrInvalidVariableName = apierror.New(apierror.KindInvalid, "invalid environment variable name").WithRetryable(apierror.False)

	ErrInvalidStorageID = apierror.New(apierror.KindInvalid, "invalid storage ID format, must have both namespace and name").WithRetryable(apierror.False)

	ErrEmptyStorageList = apierror.New(apierror.KindInvalid, "router storage must have at least one storage").WithRetryable(apierror.False)

	ErrStorageReadOnly = apierror.New(apierror.KindPermissionDenied, "storage is read-only").WithRetryable(apierror.False)

	ErrNoStorages = apierror.New(apierror.KindInvalid, "at least one storage must be provided").WithRetryable(apierror.False)

	ErrEmptyFilePath = apierror.New(apierror.KindInvalid, "file path must not be empty").WithRetryable(apierror.False)
)

func NewVariableNotFoundError(name string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "environment variable not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"variable": name}))
}

func NewStorageNotFoundError(storageID string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "environment storage backend not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"storage": storageID}))
}

func NewInvalidVariableNameError(name string, reason string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid environment variable name: "+reason).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"variable": name, "reason": reason}))
}

func NewInvalidVariableError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid variable: "+err.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewVariableNameExistsError(name string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, "variable name already exists: "+name).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}

func NewUnsupportedKindError(kind string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewDecodeConfigError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode configuration").WithCause(err).WithRetryable(apierror.False)
}

func NewInvalidConfigError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid configuration").WithCause(err).WithRetryable(apierror.False)
}

func NewStorageNotExistsError(storageID string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "storage does not exist").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"storage_id": storageID}))
}

func NewDecodeVariableError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode variable").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

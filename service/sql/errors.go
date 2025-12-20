package sql

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	ErrPoolClosed          = apierror.New(apierror.Unavailable, "connection pool is closed").WithRetryable(apierror.False)
	ErrTranscoderRequired  = apierror.New(apierror.Invalid, "transcoder is required").WithRetryable(apierror.False)
	ErrEventBusRequired    = apierror.New(apierror.Invalid, "event bus is required").WithRetryable(apierror.False)
	ErrPoolFactoryRequired = apierror.New(apierror.Invalid, "pool factory is required").WithRetryable(apierror.False)
)

func NewPingError(err error) apierror.Error {
	return apierror.New(apierror.Unavailable, "failed to ping database").WithRetryable(apierror.True).WithCause(err)
}

func NewInvalidConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid configuration").WithRetryable(apierror.False).WithCause(err)
}

func NewInvalidConfigTypeError(configType string, expectedKind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid config type").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"config_type": configType, "expected_kind": expectedKind})
}

func NewUnsupportedConfigTypeError(configType registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported config type").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"config_type": configType})
}

func NewUnsupportedAccessModeError(mode string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported access mode").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"mode": mode})
}

func NewUnsupportedDatabaseTypeError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported database type").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"database_type": kind})
}

func NewConnectionPoolCreationError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create connection pool").WithRetryable(apierror.False).WithCause(err)
}

func NewSQLiteConnectionCreationError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create SQLite connection").WithRetryable(apierror.False).WithCause(err)
}

func NewWALModeError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to enable WAL mode").WithRetryable(apierror.False).WithCause(err)
}

func NewInvalidDSNError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid connection config").WithRetryable(apierror.False).WithCause(err)
}

func NewUnsupportedEntryKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"kind": kind})
}

func NewServiceExistsError(id registry.ID) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "service already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"service_id": id.String()})
}

func NewServiceNotFoundError(id registry.ID) apierror.Error {
	return apierror.New(apierror.NotFound, "service not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"service_id": id.String()})
}

func NewInvalidPortError(envVar string, err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid port value from env").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"env_var": envVar}).
		WithCause(err)
}

func NewPoolUpdateError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to update pool config").WithRetryable(apierror.False).WithCause(err)
}

func NewSQLiteUpdateError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to update SQLite config").WithRetryable(apierror.False).WithCause(err)
}

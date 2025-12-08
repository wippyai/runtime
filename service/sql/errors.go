package sql

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var (
	ErrPoolClosed = &Error{
		kind:      apierror.KindUnavailable,
		message:   "connection pool is closed",
		retryable: apierror.False,
	}

	ErrTranscoderRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "transcoder is required",
		retryable: apierror.False,
	}

	ErrEventBusRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "event bus is required",
		retryable: apierror.False,
	}

	ErrPoolFactoryRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "pool factory is required",
		retryable: apierror.False,
	}
)

func NewPingError(err error) *Error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   "failed to ping database",
		retryable: apierror.True,
		cause:     err,
	}
}

func NewInvalidConfigError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid configuration",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewInvalidConfigTypeError(configType string, expectedKind registry.Kind) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid config type",
		retryable: apierror.False,
		details: attrs.Bag{
			"config_type":   configType,
			"expected_kind": expectedKind,
		},
	}
}

func NewUnsupportedConfigTypeError(configType registry.Kind) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported config type",
		retryable: apierror.False,
		details: attrs.Bag{
			"config_type": configType,
		},
	}
}

func NewUnsupportedAccessModeError(mode string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported access mode",
		retryable: apierror.False,
		details: attrs.Bag{
			"mode": mode,
		},
	}
}

func NewUnsupportedDatabaseTypeError(kind registry.Kind) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported database type",
		retryable: apierror.False,
		details: attrs.Bag{
			"database_type": kind,
		},
	}
}

func NewConnectionPoolCreationError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create connection pool",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewSQLiteConnectionCreationError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create SQLite connection",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewWALModeError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to enable WAL mode",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewInvalidDSNError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid connection config",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewUnsupportedEntryKindError(kind registry.Kind) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind",
		retryable: apierror.False,
		details: attrs.Bag{
			"kind": kind,
		},
	}
}

func NewServiceExistsError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "service already exists",
		retryable: apierror.False,
		details: attrs.Bag{
			"service_id": id.String(),
		},
	}
}

func NewServiceNotFoundError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "service not found",
		retryable: apierror.False,
		details: attrs.Bag{
			"service_id": id.String(),
		},
	}
}

func NewInvalidPortError(envVar string, err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid port value from env",
		retryable: apierror.False,
		details: attrs.Bag{
			"env_var": envVar,
		},
		cause: err,
	}
}

func NewPoolUpdateError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update pool config",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewSQLiteUpdateError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update SQLite config",
		retryable: apierror.False,
		cause:     err,
	}
}

package tokenstore

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
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
	ErrInvalidTokenStoreConfig = &Error{kind: apierror.KindInvalid, message: "invalid token store config"}
)

func NewDecodeTokenStoreConfigError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "failed to decode token store config",
		cause:   cause,
	}
}

func NewTokenStoreAlreadyExistsError(id string) *Error {
	return &Error{
		kind:    apierror.KindAlreadyExists,
		message: "token store " + id + " already exists",
	}
}

func NewTokenStoreNotFoundError(id string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "token store " + id + " not found",
	}
}

func NewUnsupportedEntryKindError(kind string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "unsupported entry kind: " + kind,
	}
}

func NewCreateTokenStoreError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create token store",
		cause:   cause,
	}
}

func NewAcquireBackingStoreError(store string, cause error) *Error {
	return &Error{
		kind:    apierror.KindUnavailable,
		message: "failed to acquire backing store '" + store + "'",
		cause:   cause,
	}
}

func NewGetStoreImplementationError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to get store implementation",
		cause:   cause,
	}
}

func NewResourceNotKVStoreError(resource string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "resource '" + resource + "' is not a key-value store",
	}
}

func NewGenerateTokenError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to generate token",
		cause:   cause,
	}
}

func NewStoreTokenError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to store token",
		cause:   cause,
	}
}

func NewRetrieveTokenError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to retrieve token",
		cause:   cause,
	}
}

func NewUnmarshalTokenDataError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to unmarshal token data",
		cause:   cause,
	}
}

func NewDeleteTokenError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to delete token",
		cause:   cause,
	}
}

func NewGenerateRandomTokenError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to generate random token",
		cause:   cause,
	}
}

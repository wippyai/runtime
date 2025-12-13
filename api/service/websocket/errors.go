package websocket

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for websocket operations.
var (
	ErrConnNotFound = errors.New("websocket connection not found")
	ErrConnClosed   = errors.New("websocket connection closed")
)

// Error implements apierror.Error for websocket errors.
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

// NewConnNotFoundError creates an error when connection is not found.
func NewConnNotFoundError(connID uint64) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "websocket connection not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"conn_id": connID}),
		cause:     ErrConnNotFound,
	}
}

// NewConnClosedError creates an error when connection is already closed.
func NewConnClosedError(connID uint64) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "websocket connection closed",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"conn_id": connID}),
		cause:     ErrConnClosed,
	}
}

// NewDialError creates an error when dialing fails.
func NewDialError(url string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to dial websocket",
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"url": url}),
		cause:     err,
	}
}

// NewSendError creates an error when sending message fails.
func NewSendError(connID uint64, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to send message",
		retryable: apierror.Unknown,
		details:   attrs.NewBagFrom(map[string]any{"conn_id": connID}),
		cause:     err,
	}
}

// NewReceiveError creates an error when receiving message fails.
func NewReceiveError(connID uint64, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to receive message",
		retryable: apierror.Unknown,
		details:   attrs.NewBagFrom(map[string]any{"conn_id": connID}),
		cause:     err,
	}
}

// NewCloseError creates an error when closing connection fails.
func NewCloseError(connID uint64, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to close connection",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"conn_id": connID}),
		cause:     err,
	}
}

// NewPingError creates an error when ping fails.
func NewPingError(connID uint64, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to ping",
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"conn_id": connID}),
		cause:     err,
	}
}

// NewNoRegistryError creates an error when registry is not available.
func NewNoRegistryError() *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "websocket registry not available",
		retryable: apierror.False,
	}
}

// NewNoRelayNodeError creates an error when relay node is not available.
func NewNoRelayNodeError() *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "relay node not available",
		retryable: apierror.False,
	}
}

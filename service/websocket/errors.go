package websocket

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrConnNotFound = errors.New("websocket connection not found")
	ErrConnClosed   = errors.New("websocket connection closed")
)

func NewConnNotFoundError(connID uint64) apierror.Error {
	return apierror.New(apierror.KindNotFound, "websocket connection not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"conn_id": connID})).
		WithCause(ErrConnNotFound)
}

func NewConnClosedError(connID uint64) apierror.Error {
	return apierror.New(apierror.KindInvalid, "websocket connection closed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"conn_id": connID})).
		WithCause(ErrConnClosed)
}

func NewDialError(url string, err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to dial websocket").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"url": url})).
		WithCause(err)
}

func NewSendError(connID uint64, err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to send message").
		WithRetryable(apierror.Unknown).
		WithDetails(attrs.NewBagFrom(map[string]any{"conn_id": connID})).
		WithCause(err)
}

func NewReceiveError(connID uint64, err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to receive message").
		WithRetryable(apierror.Unknown).
		WithDetails(attrs.NewBagFrom(map[string]any{"conn_id": connID})).
		WithCause(err)
}

func NewCloseError(connID uint64, err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to close connection").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"conn_id": connID})).
		WithCause(err)
}

func NewPingError(connID uint64, err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to ping").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"conn_id": connID})).
		WithCause(err)
}

func NewNoRegistryError() apierror.Error {
	return apierror.New(apierror.KindInternal, "websocket registry not available").WithRetryable(apierror.False)
}

func NewNoRelayNodeError() apierror.Error {
	return apierror.New(apierror.KindInternal, "relay node not available").WithRetryable(apierror.False)
}

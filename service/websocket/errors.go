// SPDX-License-Identifier: MPL-2.0

package websocket

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrConnNotFound = apierror.New(apierror.NotFound, "websocket connection not found").WithRetryable(apierror.False)
	ErrConnClosed   = apierror.New(apierror.Unavailable, "websocket connection closed").WithRetryable(apierror.False)
)

func NewConnNotFoundError(connID uint64) apierror.Error {
	return apierror.New(apierror.NotFound, "websocket connection not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"conn_id": connID})).
		WithCause(ErrConnNotFound)
}

func NewConnClosedError(connID uint64) apierror.Error {
	return apierror.New(apierror.Unavailable, "websocket connection closed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"conn_id": connID})).
		WithCause(ErrConnClosed)
}

func NewDialError(url string, err error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to dial websocket").
		WithRetryable(apierror.True)
	details := attrs.NewBagFrom(map[string]any{"url": url})
	if err != nil {
		details.Set("cause", err.Error())
		apiErr = apiErr.WithCause(err)
	}
	return apiErr.WithDetails(details)
}

func NewSendError(connID uint64, err error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to send message").
		WithRetryable(apierror.False)
	details := attrs.NewBagFrom(map[string]any{"conn_id": connID})
	if err != nil {
		details.Set("cause", err.Error())
		apiErr = apiErr.WithCause(err)
	}
	return apiErr.WithDetails(details)
}

func NewReceiveError(connID uint64, err error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to receive message").
		WithRetryable(apierror.False)
	details := attrs.NewBagFrom(map[string]any{"conn_id": connID})
	if err != nil {
		details.Set("cause", err.Error())
		apiErr = apiErr.WithCause(err)
	}
	return apiErr.WithDetails(details)
}

func NewPingError(connID uint64, err error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to ping").
		WithRetryable(apierror.True)
	details := attrs.NewBagFrom(map[string]any{"conn_id": connID})
	if err != nil {
		details.Set("cause", err.Error())
		apiErr = apiErr.WithCause(err)
	}
	return apiErr.WithDetails(details)
}

func NewNoRegistryError() apierror.Error {
	return apierror.New(apierror.Internal, "websocket registry not available").WithRetryable(apierror.False)
}

func NewNoRelayNodeError() apierror.Error {
	return apierror.New(apierror.Internal, "relay node not available").WithRetryable(apierror.False)
}

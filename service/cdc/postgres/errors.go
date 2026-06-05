// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	ErrUnknownRelation    = errors.New("cdc: unknown relation id")
	ErrSourceClosed       = errors.New("cdc: source is closed")
	ErrNoPublication      = errors.New("cdc: no publication and no tables configured")
	ErrTranscoderRequired = apierror.New(apierror.Invalid, "transcoder is required").WithRetryable(apierror.False)
	ErrEventBusRequired   = apierror.New(apierror.Invalid, "event bus is required").WithRetryable(apierror.False)
)

func NewUnsupportedEntryKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewServiceExistsError(id registry.ID) apierror.Error {
	return apierror.New(apierror.Conflict, "cdc service already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id.String()}))
}

func NewServiceNotFoundError(id registry.ID) apierror.Error {
	return apierror.New(apierror.NotFound, "cdc service not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id.String()}))
}

func NewInvalidConfigError(err error) apierror.Error {
	apiErr := apierror.New(apierror.Invalid, "invalid cdc configuration").WithRetryable(apierror.False)
	if err != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).WithCause(err)
	}
	return apiErr
}

func NewSourceCreationError(err error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to create cdc source").WithRetryable(apierror.False)
	if err != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).WithCause(err)
	}
	return apiErr
}

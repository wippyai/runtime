// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"errors"
	"regexp"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	ErrPoolClosed          = apierror.New(apierror.Unavailable, "connection pool is closed").WithRetryable(apierror.False)
	ErrTranscoderRequired  = apierror.New(apierror.Invalid, "transcoder is required").WithRetryable(apierror.False)
	ErrEventBusRequired    = apierror.New(apierror.Invalid, "event bus is required").WithRetryable(apierror.False)
	ErrPoolFactoryRequired = apierror.New(apierror.Invalid, "pool factory is required").WithRetryable(apierror.False)
	postgresURLPassword    = regexp.MustCompile(`(?i)\b(postgres(?:ql)?://[^/@\s:]+:)[^@/\s]*(@)`)
	postgresPassword       = regexp.MustCompile(`(?i)(\bpassword\s*=\s*)(?:'(?:\\.|[^'])*'|'.*$|[^\s&;,)]*)`)
)

func NewPingError(err error) apierror.Error {
	return withSanitizedCause(apierror.New(apierror.Unavailable, "failed to ping database").WithRetryable(apierror.True), err)
}

func NewInvalidConfigError(err error) apierror.Error {
	return withSanitizedCause(apierror.New(apierror.Invalid, "invalid configuration").WithRetryable(apierror.False), err)
}

func NewInvalidConfigTypeError(configType string, expectedKind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid config type").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"config_type":   configType,
			"expected_kind": expectedKind,
		}))
}

func NewUnsupportedConfigTypeError(configType registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported config type").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"config_type": configType}))
}

func NewUnsupportedAccessModeError(mode string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported access mode").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"mode": mode}))
}

func NewConnectionPoolCreationError(err error) apierror.Error {
	return withSanitizedCause(apierror.New(apierror.Internal, "failed to create connection pool").WithRetryable(apierror.False), err)
}

func NewWALModeError(err error) apierror.Error {
	return withSanitizedCause(apierror.New(apierror.Internal, "failed to enable WAL mode").WithRetryable(apierror.False), err)
}

func NewInvalidDSNError(err error) apierror.Error {
	return withSanitizedCause(apierror.New(apierror.Invalid, "invalid connection config").WithRetryable(apierror.False), err)
}

func NewUnsupportedEntryKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewServiceExistsError(id registry.ID) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "service already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"service_id": id.String()}))
}

func NewServiceNotFoundError(id registry.ID) apierror.Error {
	return apierror.New(apierror.NotFound, "service not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"service_id": id.String()}))
}

func NewPoolUpdateError(err error) apierror.Error {
	return withSanitizedCause(apierror.New(apierror.Internal, "failed to update pool config").WithRetryable(apierror.False), err)
}

func withSanitizedCause(apiErr apierror.Builder, err error) apierror.Error {
	if err != nil {
		cause := sanitizeCause(err)
		return apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func sanitizeCause(err error) error {
	original := err.Error()
	message := postgresPassword.ReplaceAllString(original, "${1}[REDACTED]")
	message = postgresURLPassword.ReplaceAllString(message, "${1}[REDACTED]${2}")
	if message == original {
		return err
	}
	return errors.New(message)
}

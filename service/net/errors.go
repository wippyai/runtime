// SPDX-License-Identifier: MPL-2.0

package net

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	netapi "github.com/wippyai/runtime/api/net"
)

// NewEntryDataMissingError reports that a network registry entry has no config data.
func NewEntryDataMissingError(entryID string) apierror.Error {
	return apierror.New(apierror.Invalid, "network entry "+entryID+": missing config data").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID}))
}

// NewUnsupportedKindError reports an unknown network Kind.
func NewUnsupportedKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported network kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

// NewDecodeConfigError reports a config decode failure for a driver.
func NewDecodeConfigError(driver string, err error) apierror.Error {
	return apierror.New(apierror.Invalid, driver+": failed to decode config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"driver": driver, "cause": err.Error()})).
		WithCause(err)
}

// NewConfigValidationError reports a config-validation failure for a driver.
func NewConfigValidationError(driver string, err error) apierror.Error {
	return apierror.New(apierror.Invalid, driver+": invalid config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"driver": driver, "cause": err.Error()})).
		WithCause(err)
}

// NewServiceStartError reports that a driver failed to start its underlying node or session.
func NewServiceStartError(driver string, err error) apierror.Error {
	return apierror.New(apierror.Unavailable, driver+": failed to start service").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"driver": driver, "cause": err.Error()})).
		WithCause(err)
}

// NewDialerCreateError reports a driver failed to construct its dialer.
func NewDialerCreateError(driver string, err error) apierror.Error {
	return apierror.New(apierror.Internal, driver+": failed to create dialer").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"driver": driver, "cause": err.Error()})).
		WithCause(err)
}

// NewIsolationCredentialError reports a failure generating random SOCKS5 isolation credentials.
func NewIsolationCredentialError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "socks5: failed to generate isolation credential").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewUnsupportedOperationError wraps netapi.ErrNotSupported with driver-specific context.
// errors.Is(err, netapi.ErrNotSupported) remains true through the cause chain.
func NewUnsupportedOperationError(driver, reason string) apierror.Error {
	return apierror.New(apierror.Invalid, driver+": operation not supported ("+reason+")").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"driver": driver, "reason": reason})).
		WithCause(netapi.ErrNotSupported)
}

// NewProtocolError reports a failure in a driver's control-plane protocol exchange.
func NewProtocolError(driver, phase string, err error) apierror.Error {
	return apierror.New(apierror.Unavailable, driver+": "+phase+" failed").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"driver": driver, "phase": phase, "cause": err.Error()})).
		WithCause(err)
}

// NewProtocolRejectError reports that a driver's peer rejected a control-plane request.
func NewProtocolRejectError(driver, phase, response string) apierror.Error {
	return apierror.New(apierror.Unavailable, driver+": "+phase+" rejected: "+response).
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"driver": driver, "phase": phase, "response": response}))
}

// NewEnvRegistryUnavailableError reports that a driver config references an env
// var but the manager has no env registry wired in to resolve it.
func NewEnvRegistryUnavailableError(envVar string) apierror.Error {
	return apierror.New(apierror.Internal, "env registry unavailable: cannot resolve "+envVar).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"env_var": envVar}))
}

// NewAuthKeyLookupError reports a failure to resolve an auth-key env var via the env registry.
func NewAuthKeyLookupError(envVar string, err error) apierror.Error {
	return apierror.New(apierror.NotFound, "failed to resolve auth key env var "+envVar).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"env_var": envVar, "cause": err.Error()})).
		WithCause(err)
}

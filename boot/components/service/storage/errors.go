// SPDX-License-Identifier: MPL-2.0

package storage

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable           = apierror.New(apierror.Internal, "logger not available in context").WithRetryable(apierror.False)
	ErrTranscoderNotAvailable       = apierror.New(apierror.Internal, "transcoder not available in context").WithRetryable(apierror.False)
	ErrEventBusNotAvailable         = apierror.New(apierror.Internal, "event bus not available in context").WithRetryable(apierror.False)
	ErrResourceRegistryNotAvailable = apierror.New(apierror.Internal, "resource registry not available in context").WithRetryable(apierror.False)
	ErrSecurityRegistryNotAvailable = apierror.New(apierror.Internal, "security registry not available in context").WithRetryable(apierror.False)
	ErrHandlerRegistryNotAvailable  = apierror.New(apierror.Internal, "handler registry not available in context").WithRetryable(apierror.False)
	ErrRegistryNotAvailable         = apierror.New(apierror.Internal, "registry not available in context").WithRetryable(apierror.False)
)

func NewSQLManagerError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create sql manager").WithCause(cause)
}

func NewCDCManagerError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create cdc manager").WithCause(cause)
}

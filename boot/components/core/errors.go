// SPDX-License-Identifier: MPL-2.0

package core

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable           = apierror.New(apierror.Internal, "logger not available in context").WithRetryable(apierror.False)
	ErrEventBusNotAvailable         = apierror.New(apierror.Internal, "event bus not available in context").WithRetryable(apierror.False)
	ErrRegistryNotAvailable         = apierror.New(apierror.Internal, "registry not available in context").WithRetryable(apierror.False)
	ErrTranscoderNotAvailable       = apierror.New(apierror.Internal, "transcoder not available in context").WithRetryable(apierror.False)
	ErrArtifactRegistryNotAvailable = apierror.New(apierror.Internal, "artifact registry is not initialized").WithRetryable(apierror.False)
)

// NewDependencyRestoreError reports a failed startup dependency restore. The
// cause carries the actionable detail, so it stays in the chain.
func NewDependencyRestoreError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to prepare dependency restore").WithCause(cause)
}

func NewWorkspaceReplacementsError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load workspace replacements").WithCause(cause)
}

func NewHistoryPathError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to resolve history path").WithCause(cause)
}

func NewSQLiteHistoryError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create SQLite history").WithCause(cause)
}

func NewPostgresHistoryError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create PostgreSQL history").WithCause(cause)
}

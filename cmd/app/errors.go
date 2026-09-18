// SPDX-License-Identifier: MPL-2.0

package app

import (
	"strconv"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// ErrBusy means another invocation owns the selected application state.
// It says nothing about the owner's identity, readiness or ability to accept clients.
var ErrBusy apierror.Error = apierror.New(apierror.Conflict, "application state is owned")

// ErrEmptyDefaultState reports an executable whose default state resolver
// selected no directory.
var ErrEmptyDefaultState apierror.Error = apierror.New(apierror.Invalid, "application default state directory is empty").
	WithRetryable(apierror.False)

// NewDefaultStateResolveError reports an executable whose default state
// resolver failed, so no state directory can be selected.
func NewDefaultStateResolveError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "application default state directory could not be resolved").
		WithRetryable(apierror.False).
		WithCause(cause)
}

// NewOwnedStateError reports that another invocation holds the application
// state directory, carrying the lock attempt that observed it.
func NewOwnedStateError(cause error) apierror.Error {
	return apierror.New(apierror.Conflict, "application state is owned").
		WithRetryable(apierror.True).
		WithCause(cause)
}

// NewInvalidApplicationNameError reports an executable whose application name
// cannot address a state directory.
func NewInvalidApplicationNameError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid application name").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}

// NewInvalidApplicationModeError reports an executable that declares a
// deployment mode the runner does not implement.
func NewInvalidApplicationModeError(mode string) apierror.Error {
	return apierror.New(apierror.Invalid, "application mode must be base or bootstrap").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"mode": mode}))
}

// NewMissingApplicationCommandError reports an executable that names no
// default application command.
func NewMissingApplicationCommandError() apierror.Error {
	return apierror.New(apierror.Invalid, "application command is required").
		WithRetryable(apierror.False)
}

// NewInvalidBaselineError reports an executable that selects startup code the
// runner does not implement.
func NewInvalidBaselineError(baseline string) apierror.Error {
	return apierror.New(apierror.Invalid, "application baseline must be activated or embedded").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"baseline": baseline}))
}

// NewDataEnvironmentBindingError reports a data environment entry whose name
// or relative path cannot bind to a path inside the state directory.
func NewDataEnvironmentBindingError(name, path string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid application data environment binding "+strconv.Quote(name)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name, "path": path}))
}

// NewBaseDeploymentUnavailableError reports base recovery requested from an
// executable that carries no independent baseline.
func NewBaseDeploymentUnavailableError(mode string) apierror.Error {
	return apierror.New(apierror.Invalid, "bootstrap applications do not expose a base deployment").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"mode": mode}))
}

// NewBaseUpdateRejectedError reports an update requested against the embedded
// baseline, which the executable replaces rather than updates.
func NewBaseUpdateRejectedError() apierror.Error {
	return apierror.New(apierror.Invalid, "base recovery cannot be updated").
		WithRetryable(apierror.False)
}

// NewBaseRuntimeRejectedError reports Wippy CLI tooling requested under base
// recovery, which only starts the embedded application.
func NewBaseRuntimeRejectedError() apierror.Error {
	return apierror.New(apierror.Invalid, "base recovery only runs the embedded application").
		WithRetryable(apierror.False)
}

// NewLaunchIncompleteError reports a launch callback that returned while its
// owner runner was still executing.
func NewLaunchIncompleteError() apierror.Error {
	return apierror.New(apierror.Internal, "application launch returned before owner runner completed")
}

// NewOwnerRunnerReusedError reports an owner runner invoked more than once or
// after its launch callback returned.
func NewOwnerRunnerReusedError() apierror.Error {
	return apierror.New(apierror.Invalid, "application owner runner is single-use and valid only during launch").
		WithRetryable(apierror.False)
}

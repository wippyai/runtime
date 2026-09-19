// SPDX-License-Identifier: MPL-2.0

package app

import (
	"strconv"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// ErrOwned means another invocation owns the selected application state.
// It says nothing about the owner's identity, readiness or ability to accept
// clients. Every owned-state failure matches it with errors.Is.
var ErrOwned apierror.Error = apierror.New(apierror.Conflict, "application state is owned")

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

// NewMissingApplicationCommandError reports an executable that names no
// default application command.
func NewMissingApplicationCommandError() apierror.Error {
	return apierror.New(apierror.Invalid, "application command is required").
		WithRetryable(apierror.False)
}

// NewMissingStateDirectoryError reports a --state flag that names no directory.
func NewMissingStateDirectoryError() apierror.Error {
	return apierror.New(apierror.Invalid, "--state requires a state directory").
		WithRetryable(apierror.False)
}

// NewDataEnvironmentBindingError reports a data environment entry whose name
// or relative path cannot bind to a path inside the state directory.
func NewDataEnvironmentBindingError(name, path string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid application data environment binding "+strconv.Quote(name)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name, "path": path}))
}

// NewBundledPackError reports a shipped pack that does not carry the identity
// or the content the bundle declares for it. Detail names the part of the pack
// the executable and the bundle disagree about.
func NewBundledPackError(module, detail string, cause error) apierror.Error {
	message := "pack " + module
	if detail != "" {
		message += " " + detail
	}
	return apierror.New(apierror.Invalid, message).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"module": module, "detail": detail})).
		WithCause(cause)
}

// NewDuplicateBundledModuleError reports a module a bundle ships twice, which
// leaves the version the deployment selects undecided.
func NewDuplicateBundledModuleError(module string) apierror.Error {
	return apierror.New(apierror.Invalid, "duplicate bundled module "+module).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"module": module}))
}

// NewMissingBundledApplicationError reports a bundle whose packs do not
// include the application it selects as its root.
func NewMissingBundledApplicationError(root string) apierror.Error {
	return apierror.New(apierror.Invalid, "bundled application "+root+" is missing").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"root": root}))
}

// NewExistingDeploymentLockError reports a deployment directory whose lock file
// the executable cannot read.
func NewExistingDeploymentLockError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "existing deployment lock").
		WithCause(cause)
}

// NewDeploymentApplicationError reports a deployment that selects an
// application other than the one the executable ships.
func NewDeploymentApplicationError(root string) apierror.Error {
	return apierror.New(apierror.Invalid, "deployment does not select "+root).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"root": root}))
}

// NewApplicationStateError reports an operation on the application state
// directory that could not be completed.
func NewApplicationStateError(operation, target string, cause error) apierror.Error {
	details := map[string]any{"operation": operation}
	subject := operation
	if target != "" {
		details["target"] = target
		subject = operation + " " + target
	}
	return apierror.New(apierror.Internal, "application state operation failed: "+subject).
		WithDetails(attrs.NewBagFrom(details)).
		WithCause(cause)
}

// NewCurrentDeploymentError reports a current record that does not name a
// deployment this state holds.
func NewCurrentDeploymentError(detail, path string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "current deployment record is not usable: "+detail).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"detail": detail, "path": path})).
		WithCause(cause)
}

// NewRetainedDeploymentError reports a retained deployment that does not hold
// the immutable shape the artifact cache reads it as.
func NewRetainedDeploymentError(detail, path string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "retained deployment is not usable: "+detail).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"detail": detail, "path": path})).
		WithCause(cause)
}

// NewModuleArtifactError reports a module record whose name, version or digest
// does not identify the artifact it pins.
func NewModuleArtifactError(detail, module, version string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "module artifact identity is not usable: "+detail+": "+module+"@"+version).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"detail": detail, "module": module, "version": version})).
		WithCause(cause)
}

// NewArtifactCacheError reports content that could not be placed in the
// application artifact cache.
func NewArtifactCacheError(detail, subject string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "artifact cache operation failed: "+detail+" "+subject).
		WithDetails(attrs.NewBagFrom(map[string]any{"detail": detail, "subject": subject})).
		WithCause(cause)
}

// NewUpdateError reports an update stage that failed. The active deployment
// stays the one the state already selects.
func NewUpdateError(stage string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "update stage "+stage+" failed; the active deployment is unchanged").
		WithDetails(attrs.NewBagFrom(map[string]any{"stage": stage})).
		WithCause(cause)
}

// NewUpdatedModuleError reports a module the update candidate selects whose
// artifact is missing or does not carry the content the lock pins.
func NewUpdatedModuleError(detail, module string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "updated module "+module+" "+detail+"; the active deployment is unchanged").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"detail": detail, "module": module})).
		WithCause(cause)
}

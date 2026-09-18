// SPDX-License-Identifier: MPL-2.0

package app

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

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

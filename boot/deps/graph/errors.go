// SPDX-License-Identifier: MPL-2.0

package graph

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrNoLabelsAvailable        = apierror.New(apierror.NotFound, "no labels available").WithRetryable(apierror.False)
	ErrNoConstraints            = apierror.New(apierror.Invalid, "no constraints to merge").WithRetryable(apierror.False)
	ErrManifestProviderRequired = apierror.New(apierror.Invalid, "manifest provider is required").WithRetryable(apierror.False)
)

func NewParseConstraintError(constraint string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("failed to parse constraint %q", constraint)).WithCause(cause)
}

func NewNoMatchingVersionError(constraint string) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("no version matches constraint %q", constraint))
}

func NewIncompatibleConstraintsError(c1, c2 string) apierror.Error {
	return apierror.New(apierror.Conflict, fmt.Sprintf("constraints %q and %q are incompatible", c1, c2))
}

func NewInvalidModuleNameError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid module name format: "+name+" (expected org/module)").
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}

func NewEmptyModuleNameError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, "empty organization or module name: "+name).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}

func NewFetchManifestsError(level int, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("fetch manifests level %d", level)).
		WithCause(cause).
		WithDetails(attrs.NewBagFrom(map[string]any{"level": level}))
}

func NewFetchManifestError(name Name, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "fetch manifest "+name.String()).
		WithCause(cause).
		WithDetails(attrs.NewBagFrom(map[string]any{"module": name.String()}))
}

func NewNoMatchingVersionForModuleError(name Name, constraint string) apierror.Error {
	return apierror.New(apierror.NotFound, "no matching version for "+name.String()+" with constraint "+constraint).
		WithDetails(attrs.NewBagFrom(map[string]any{"module": name.String(), "constraint": constraint}))
}

func NewCircularDependencyError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "circular dependency detected").WithCause(cause)
}

// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	// ErrScopeAlreadyExists is returned when trying to add a scope that already exists.
	ErrScopeAlreadyExists = apierror.New(apierror.AlreadyExists, "pg scope already exists").WithRetryable(apierror.False)

	// ErrScopeNotFound is returned when trying to update or delete a scope that doesn't exist.
	ErrScopeNotFound = apierror.New(apierror.NotFound, "pg scope not found").WithRetryable(apierror.False)
)

// NewDecodeConfigError creates an error for config decode failures.
func NewDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode pg scope config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

// NewUnsupportedKindError creates an error for unsupported entry kinds.
func NewUnsupportedKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind for pg").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

// NewScopeAlreadyExistsError creates a scope already exists error with ID context.
func NewScopeAlreadyExistsError(id string) apierror.Error {
	return ErrScopeAlreadyExists.WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

// NewScopeNotFoundError creates a scope not found error with ID context.
func NewScopeNotFoundError(id string) apierror.Error {
	return ErrScopeNotFound.WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

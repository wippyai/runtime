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

// Service-instance errors, surfaced by the PG engine (not the scope manager).
var (
	// ErrNotJoined is returned when a process tries to leave a group it hasn't joined.
	ErrNotJoined = apierror.New(apierror.NotFound, "pg: process not joined in group").WithRetryable(apierror.False)

	// ErrGroupNotFound is returned when querying a non-existent group.
	ErrGroupNotFound = apierror.New(apierror.NotFound, "pg: group not found").WithRetryable(apierror.False)

	// ErrServiceStopped is returned when the pg service is not running.
	ErrServiceStopped = apierror.New(apierror.Unavailable, "pg: service stopped").WithRetryable(apierror.False)

	// ErrBackpressure is returned when the event loop queue is full.
	ErrBackpressure = apierror.New(apierror.Unavailable, "pg: event loop backpressure, try again later").WithRetryable(apierror.True)
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

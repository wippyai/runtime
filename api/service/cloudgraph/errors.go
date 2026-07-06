// SPDX-License-Identifier: MPL-2.0

package cloudgraph

import apierror "github.com/wippyai/runtime/api/error"

// Validation errors for cloud-graph configuration. All are non-retryable and
// surface during entry decoding.
var (
	ErrWorkerRequired        = apierror.New(apierror.Invalid, "worker reference is required").WithRetryable(apierror.False)
	ErrClientRequired        = apierror.New(apierror.Invalid, "client reference is required").WithRetryable(apierror.False)
	ErrDatabaseRequired      = apierror.New(apierror.Invalid, "database reference is required").WithRetryable(apierror.False)
	ErrHostRequired          = apierror.New(apierror.Invalid, "host reference is required").WithRetryable(apierror.False)
	ErrProviderTypeRequired  = apierror.New(apierror.Invalid, "provider_type is required").WithRetryable(apierror.False)
	ErrActorProcessRequired  = apierror.New(apierror.Invalid, "actor_process reference is required").WithRetryable(apierror.False)
	ErrResourceTypesRequired = apierror.New(apierror.Invalid, "resource_types must not be empty").WithRetryable(apierror.False)
)

// NewProviderNotFoundError reports a provider_type with no registered manifest.
// Resolution fails hard by design; there is no fallback provider.
func NewProviderNotFoundError(providerType string) apierror.Error {
	return apierror.New(apierror.NotFound, "no provider registered for provider_type: "+providerType).WithRetryable(apierror.False)
}

// NewProviderConflictError reports a second manifest claiming an existing
// provider_type, which is a hard configuration error.
func NewProviderConflictError(providerType string) apierror.Error {
	return apierror.New(apierror.Conflict, "provider_type already registered: "+providerType).WithRetryable(apierror.False)
}

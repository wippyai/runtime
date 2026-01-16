package host

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

// ErrEmptyNamespace is returned when registering a host with empty namespace.
var ErrEmptyNamespace = apierror.New(apierror.Invalid, "host namespace cannot be empty").
	WithRetryable(apierror.False)

// NewHostAlreadyRegisteredError creates an error for duplicate host registration.
func NewHostAlreadyRegisteredError(namespace string) error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("host %q already registered", namespace)).
		WithRetryable(apierror.False)
}

// NewInstantiateHostError creates an error for host instantiation failures.
func NewInstantiateHostError(namespace string, err error) error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to instantiate host %q", namespace)).
		WithCause(err).
		WithRetryable(apierror.False)
}

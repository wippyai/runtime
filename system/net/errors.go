// SPDX-License-Identifier: MPL-2.0

package net

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// NewNetworkRegistryMissingError creates an error when an overlay network is selected without a registry.
func NewNetworkRegistryMissingError(networkID string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("network %q selected without a network registry", networkID)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"network_id": networkID}))
}

// NewNetworkLookupError creates an error when resolving an overlay network fails.
func NewNetworkLookupError(networkID string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("network %q lookup failed", networkID)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"network_id": networkID, "cause": cause.Error()})).
		WithCause(cause)
}

// NewInvalidAddressError creates an error when an address is invalid.
func NewInvalidAddressError(address string, cause error) apierror.Error {
	b := apierror.New(apierror.Invalid, fmt.Sprintf("invalid address %q", address)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"address": address}))
	if cause != nil {
		b = b.WithCause(cause)
	}
	return b
}

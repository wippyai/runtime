package contract

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// NewContractLoadError creates an error when loading a contract fails.
func NewContractLoadError(contractID registry.ID, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to load contract '"+contractID.String()+"': "+err.Error(),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"contract_id": contractID.String(), "cause": err.Error()}),
		err,
	)
}

// NewSubscriberError creates an error for subscriber creation failures.
func NewSubscriberError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to create subscriber: "+err.Error(),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

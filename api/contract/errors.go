package contract

import (
	"errors"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors for contract operations.
var (
	ErrInstantiatorNotFound = errors.New("contract instantiator not found in context")
	ErrInstanceNil          = errors.New("contract instance is nil")
	ErrNodeNotFound         = errors.New("relay node not found")
	ErrPIDNotFound          = errors.New("process PID not found in context")
)

// NewMethodNotBoundError creates an error when a method is not bound.
func NewMethodNotBoundError(method string) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"method '"+method+"' not bound",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"method": method}),
		nil,
	)
}

// NewMissingContextKeysError creates an error when required context keys are missing.
func NewMissingContextKeysError(keys []string) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"missing required context keys: ["+strings.Join(keys, ", ")+"]",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"missing_keys": keys}),
		nil,
	)
}

// NewContractNotFoundError creates an error when contract definition is not found.
func NewContractNotFoundError(id registry.ID) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"contract definition '"+id.String()+"' not found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"contract_id": id.String()}),
		nil,
	)
}

// NewBindingNotFoundError creates an error when contract binding is not found.
func NewBindingNotFoundError(id registry.ID) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"contract binding '"+id.String()+"' not found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
		nil,
	)
}

// NewNoDefaultBindingError creates an error when no default binding exists for a contract.
func NewNoDefaultBindingError(contractID registry.ID) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"no default binding for contract '"+contractID.String()+"'",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"contract_id": contractID.String()}),
		nil,
	)
}

// NewMethodNotFoundError creates an error when a method is not found in a contract.
func NewMethodNotFoundError(method string, contractID registry.ID) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"method '"+method+"' not found in contract '"+contractID.String()+"'",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"method": method, "contract_id": contractID.String()}),
		nil,
	)
}

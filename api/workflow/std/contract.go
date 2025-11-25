package std

import "github.com/wippyai/runtime/api/registry"

// ContractCallHeader is the header payload for contract.call commands.
// This is serialized as Params[0], with method arguments in Params[1:].
type ContractCallHeader struct {
	// BindingID is the contract binding registry ID to invoke.
	BindingID registry.ID `json:"binding_id"`

	// Method is the contract method name to invoke.
	Method string `json:"method"`

	// Context contains application context values to pass.
	Context map[string]any `json:"context,omitempty"`

	// Security contains the security context for this call.
	Security *SecurityContext `json:"security,omitempty"`

	// Options contains execution options for the contract call.
	Options *ContractCallOptions `json:"options,omitempty"`
}

// ContractCallOptions defines execution options for contract calls.
type ContractCallOptions struct {
	// Timeout as duration string (e.g., "30s").
	Timeout string `json:"timeout,omitempty"`

	// Retry defines retry policy for the contract call.
	Retry *RetryPolicy `json:"retry,omitempty"`
}

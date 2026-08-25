// SPDX-License-Identifier: MPL-2.0

package di

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/registry"
)

// A refused binding names the binding and the contract in the error text
// itself. The details bag carries the same identities for structured
// consumers, but log lines and governance surfaces render only the message,
// and a refusal that names no entry cannot be acted on.
func TestBindingErrorsNameTheirEntries(t *testing.T) {
	binding := registry.NewID("acme.app", "store_binding")
	contract := registry.NewID("acme.platform", "store_contract")
	existing := registry.NewID("acme.app", "first_binding")
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{"empty contract list", NewBindingNoContractsError(binding), []string{binding.String()}},
		{"undefined contract", NewContractNotFoundError(binding, 0, contract), []string{binding.String(), contract.String()}},
		{"unbound method", NewMethodNotBoundError(binding, contract, "get"), []string{binding.String(), contract.String(), "get"}},
		{"undefined method", NewMethodNotDefinedError(binding, contract, "put"), []string{binding.String(), contract.String(), "put"}},
		{"duplicate default", NewDuplicateDefaultBindingError(contract, existing, binding), []string{contract.String(), existing.String(), binding.String()}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, want := range test.want {
				assert.Contains(t, test.err.Error(), want)
			}
		})
	}
}

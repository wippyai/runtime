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

	assert.Contains(t, NewContractNotFoundError(binding, 0, contract).Error(), binding.String())
	assert.Contains(t, NewContractNotFoundError(binding, 0, contract).Error(), contract.String())

	assert.Contains(t, NewBindingNoContractsError(binding).Error(), binding.String())

	assert.Contains(t, NewMethodNotBoundError(binding, contract, "get").Error(), binding.String())
	assert.Contains(t, NewMethodNotBoundError(binding, contract, "get").Error(), "get")

	assert.Contains(t, NewMethodNotDefinedError(binding, contract, "put").Error(), contract.String())
	assert.Contains(t, NewMethodNotDefinedError(binding, contract, "put").Error(), "put")

	existing := registry.NewID("acme.app", "first_binding")
	assert.Contains(t, NewDuplicateDefaultBindingError(contract, existing, binding).Error(), contract.String())
	assert.Contains(t, NewDuplicateDefaultBindingError(contract, existing, binding).Error(), existing.String())
}

package contract

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/registry"
)

func TestContractLoadError(t *testing.T) {
	cause := assert.AnError
	contractID := registry.NewID("test", "contract")

	err := NewContractLoadError(contractID, cause)

	assert.Contains(t, err.Error(), "test:contract")
	assert.Contains(t, err.Error(), "failed to load")
	assert.True(t, errors.Is(err, cause))
	assert.NotNil(t, err.Details())
}

func TestSubscriberError(t *testing.T) {
	cause := assert.AnError
	err := NewSubscriberError(cause)

	assert.Contains(t, err.Error(), "subscriber")
	assert.True(t, errors.Is(err, cause))
	assert.NotNil(t, err.Details())
}

func TestMethodNotBoundError(t *testing.T) {
	err := NewMethodNotBoundError("testMethod")

	assert.Contains(t, err.Error(), "testMethod")
	assert.Contains(t, err.Error(), "not bound")
	assert.NotNil(t, err.Details())
	val, _ := err.Details().Get("method")
	assert.Equal(t, "testMethod", val)
}

func TestMissingContextKeysError(t *testing.T) {
	keys := []string{"key1", "key2", "key3"}
	err := NewMissingContextKeysError(keys)

	assert.Contains(t, err.Error(), "key1")
	assert.Contains(t, err.Error(), "key2")
	assert.Contains(t, err.Error(), "key3")
	assert.Contains(t, err.Error(), "missing required context keys")
	assert.NotNil(t, err.Details())
}

func TestContractNotFoundError(t *testing.T) {
	contractID := registry.NewID("ns", "name")
	err := NewContractNotFoundError(contractID)

	assert.Contains(t, err.Error(), "ns:name")
	assert.Contains(t, err.Error(), "not found")
	assert.NotNil(t, err.Details())
	val, _ := err.Details().Get("contract_id")
	assert.Equal(t, "ns:name", val)
}

func TestBindingNotFoundError(t *testing.T) {
	bindingID := registry.NewID("bindings", "impl")
	err := NewBindingNotFoundError(bindingID)

	assert.Contains(t, err.Error(), "bindings:impl")
	assert.Contains(t, err.Error(), "not found")
	assert.NotNil(t, err.Details())
	val, _ := err.Details().Get("binding_id")
	assert.Equal(t, "bindings:impl", val)
}

func TestNoDefaultBindingError(t *testing.T) {
	contractID := registry.NewID("contracts", "service")
	err := NewNoDefaultBindingError(contractID)

	assert.Contains(t, err.Error(), "contracts:service")
	assert.Contains(t, err.Error(), "no default binding")
	assert.NotNil(t, err.Details())
	val, _ := err.Details().Get("contract_id")
	assert.Equal(t, "contracts:service", val)
}

func TestMethodNotFoundError(t *testing.T) {
	contractID := registry.NewID("contracts", "api")
	err := NewMethodNotFoundError("getData", contractID)

	assert.Contains(t, err.Error(), "getData")
	assert.Contains(t, err.Error(), "contracts:api")
	assert.Contains(t, err.Error(), "not found")
	assert.NotNil(t, err.Details())
	method, _ := err.Details().Get("method")
	assert.Equal(t, "getData", method)
	cid, _ := err.Details().Get("contract_id")
	assert.Equal(t, "contracts:api", cid)
}

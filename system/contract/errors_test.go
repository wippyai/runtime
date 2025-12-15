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

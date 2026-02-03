package process

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnknownCommandError(t *testing.T) {
	err := NewUnknownCommandError(999)

	assert.Equal(t, "unknown command", err.Error())
	assert.Equal(t, "NotFound", string(err.Kind()))
	assert.Equal(t, "False", err.Retryable().String())

	details := err.Details()
	assert.NotNil(t, details)
	cmdID, ok := details.Get("command_id")
	assert.True(t, ok)
	assert.Equal(t, 999, cmdID)

	details2 := err.Details()
	assert.Equal(t, details, details2)
}

func TestNewInvalidFactoryEntryError(t *testing.T) {
	err := NewInvalidFactoryEntryError("test:factory")

	assert.Contains(t, err.Error(), "invalid factory entry")
	assert.Equal(t, "Internal", string(err.Kind()))

	details := err.Details()
	assert.NotNil(t, details)
	factoryID, ok := details.Get("factory_id")
	assert.True(t, ok)
	assert.Equal(t, "test:factory", factoryID)
}

func TestNewSubscriberError(t *testing.T) {
	cause := errors.New("subscription failed")
	err := NewSubscriberError(cause)

	assert.Contains(t, err.Error(), "failed to create subscriber")
	assert.Equal(t, "Internal", string(err.Kind()))
	assert.True(t, errors.Is(err, cause))
	detailCause, _ := err.Details().Get("cause")
	assert.Equal(t, "subscription failed", detailCause)
}

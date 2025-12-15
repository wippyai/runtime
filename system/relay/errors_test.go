package relay

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewInvalidHostTypeError(t *testing.T) {
	err := NewInvalidHostTypeError("host1", "node1")
	assert.Contains(t, err.Error(), "invalid type")
	assert.Equal(t, "Internal", err.Kind().String())
}

func TestNewSubscriberError(t *testing.T) {
	cause := errors.New("subscriber error")
	err := NewSubscriberError(cause)
	assert.Contains(t, err.Error(), "failed to create subscriber")
	assert.True(t, err.Retryable().Bool())
	assert.True(t, errors.Is(err, cause))
}

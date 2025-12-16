package process

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnknownCommandError(t *testing.T) {
	err := NewUnknownCommandError(999)

	assert.Equal(t, "unknown command: 999", err.Error())
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

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/payload"
)

func TestStateString(t *testing.T) {
	tests := []struct {
		state    updateState
		expected string
	}{
		{updatePending, "pending"},
		{updateAccepted, "accepted"},
		{updateRejected, "rejected"},
		{updateComplete, "completed"},
		{updateState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := stateString(tt.state)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractErrorMessage(t *testing.T) {
	tests := []struct {
		name       string
		payloads   payload.Payloads
		defaultMsg string
		expected   string
	}{
		{
			name:       "empty payloads returns default",
			payloads:   nil,
			defaultMsg: "default error",
			expected:   "default error",
		},
		{
			name:       "string payload returns string",
			payloads:   payload.Payloads{payload.NewString("custom error")},
			defaultMsg: "default",
			expected:   "custom error",
		},
		{
			name:       "non-string payload returns formatted",
			payloads:   payload.Payloads{payload.New(123)},
			defaultMsg: "default",
			expected:   "123",
		},
		{
			name:       "nil data payload returns formatted nil",
			payloads:   payload.Payloads{payload.New(nil)},
			defaultMsg: "default",
			expected:   "<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractErrorMessage(tt.payloads, tt.defaultMsg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUpdateStateConstants(t *testing.T) {
	assert.NotEqual(t, updatePending, updateAccepted)
	assert.NotEqual(t, updateAccepted, updateRejected)
	assert.NotEqual(t, updateRejected, updateComplete)

	assert.Equal(t, updateState(0), updatePending)
	assert.Equal(t, updateState(1), updateAccepted)
	assert.Equal(t, updateState(2), updateRejected)
	assert.Equal(t, updateState(3), updateComplete)
}

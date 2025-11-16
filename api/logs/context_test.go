// Package logs provides logging and log management.
package logs

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestWithLogger tests the WithLogger function
func TestWithLogger(t *testing.T) {
	// Create a test logger
	logger := zap.NewExample()

	// Create a context with AppContext
	ctx := ctxapi.NewRootContext()
	ctxWithLogger := WithLogger(ctx, logger)

	// Verify the logger was stored
	storedLogger := GetLogger(ctxWithLogger)
	assert.NotNil(t, storedLogger)
	assert.Equal(t, logger, storedLogger)
}

// TestGetLogger tests the GetLogger function
func TestGetLogger(t *testing.T) {
	tests := []struct {
		name            string
		setupContext    func() context.Context
		expectNopLogger bool
	}{
		{
			name: "context with logger",
			setupContext: func() context.Context {
				ctx := ctxapi.NewRootContext()
				return WithLogger(ctx, zap.NewExample())
			},
			expectNopLogger: false,
		},
		{
			name:            "context without logger",
			setupContext:    ctxapi.NewRootContext,
			expectNopLogger: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupContext()
			logger := GetLogger(ctx)

			assert.NotNil(t, logger, "logger should never be nil")

			if tt.expectNopLogger {
				// Test if it's a nop logger by checking if it's the same type
				assert.IsType(t, zap.NewNop(), logger)
			} else {
				assert.NotEqual(t, zap.NewNop(), logger)
			}
		})
	}
}

// MockCore implements the Core interface for testing
type MockCore struct {
	zapcore.Core
	config Config
}

func (m *MockCore) Configure(cfg Config) {
	m.config = cfg
}

func (m *MockCore) GetConfig() Config {
	return m.config
}

// TestCoreInterface tests the Core interface implementation
func TestCoreInterface(t *testing.T) {
	mockCore := &MockCore{}

	// Test configuration
	testConfig := Config{
		PropagateDownstream: true,
		StreamToEvents:      true,
		MinLevel:            zapcore.InfoLevel,
	}

	// Configure the core
	mockCore.Configure(testConfig)

	// Verify the configuration was stored
	currentConfig := mockCore.GetConfig()
	assert.Equal(t, testConfig.PropagateDownstream, currentConfig.PropagateDownstream)
	assert.Equal(t, testConfig.StreamToEvents, currentConfig.StreamToEvents)
	assert.Equal(t, testConfig.MinLevel, currentConfig.MinLevel)
}

// TestConfigJSON tests the JSON marshaling of Config struct
func TestConfigJSON(t *testing.T) {
	config := Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.WarnLevel,
	}

	// Marshal the config to JSON
	bytes, err := json.Marshal(config)
	assert.NoError(t, err)

	// Convert to string for easier assertion
	jsonStr := string(bytes)

	// Test that the JSON contains all fields with correct tags
	assert.Contains(t, jsonStr, `"propagate_downstream":true`)
	assert.Contains(t, jsonStr, `"stream_to_events":false`)
	assert.Contains(t, jsonStr, `"min_level":"warn"`)

	// Test unmarshaling
	var unmarshaledConfig Config
	err = json.Unmarshal(bytes, &unmarshaledConfig)
	assert.NoError(t, err)

	// Verify the unmarshaled values match the original
	assert.Equal(t, config.PropagateDownstream, unmarshaledConfig.PropagateDownstream)
	assert.Equal(t, config.StreamToEvents, unmarshaledConfig.StreamToEvents)
	assert.Equal(t, config.MinLevel, unmarshaledConfig.MinLevel)
}

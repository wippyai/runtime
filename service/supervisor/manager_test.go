package supervisor

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	processapi "github.com/ponyruntime/pony/api/service/supervisor"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/ponyruntime/pony/system/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// MockTranscoder for testing
type MockTranscoder struct{}

func (m *MockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	// Simple mock that just returns the same payload with the target format
	return payload.NewPayload(p.Data(), format), nil
}

func (m *MockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	// Simple mock that just copies data if it's a map
	if data, ok := p.Data().(map[string]interface{}); ok {
		// This is a simplified unmarshal for testing
		if cfg, ok := v.(*processapi.ServiceConfig); ok {
			if processID, ok := data["process"].(string); ok {
				cfg.Process = registry.ParseID(processID)
			}
			if hostID, ok := data["host"].(string); ok {
				cfg.HostID = hostID
			}
			if input, ok := data["input"].([]interface{}); ok {
				cfg.Input = input
			}
		}
	}
	return nil
}

func TestManager_Add_WithDebugLogging(t *testing.T) {
	// Create a logger that captures debug output
	logger := zap.NewNop() // In a real test, you might want to use a test logger

	// Create event bus
	bus := eventbus.NewBus()

	// Create process manager
	procManager := &process.Manager{}

	// Create supervisor manager
	manager := NewSupervisorServiceManager(bus, procManager, logger)

	// Create a test context with transcoder
	ctx := context.Background()
	ctx = payload.WithTranscoder(ctx, &MockTranscoder{})

	// Create a test entry
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "test-service"},
		Kind: processapi.KindProcessService,
		Data: payload.NewPayload(map[string]interface{}{
			"process": "test:test-process",
			"host":    "test-host",
			"input":   []interface{}{"arg1", "arg2"},
			"lifecycle": map[string]interface{}{
				"auto_start": true,
			},
		}, payload.JSON),
	}

	// Test the Add method
	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Verify the service was added
	_, exists := manager.services.Load(entry.ID)
	assert.True(t, exists, "Service should be stored in the manager")
}

func TestManager_Update_WithDebugLogging(t *testing.T) {
	// Create a logger that captures debug output
	logger := zap.NewNop()

	// Create event bus
	bus := eventbus.NewBus()

	// Create process manager
	procManager := &process.Manager{}

	// Create supervisor manager
	manager := NewSupervisorServiceManager(bus, procManager, logger)

	// Create a test context with transcoder
	ctx := context.Background()
	ctx = payload.WithTranscoder(ctx, &MockTranscoder{})

	// Create a test entry
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "test-service"},
		Kind: processapi.KindProcessService,
		Data: payload.NewPayload(map[string]interface{}{
			"process": "test:test-process",
			"host":    "test-host",
			"input":   []interface{}{"arg1", "arg2"},
			"lifecycle": map[string]interface{}{
				"auto_start": true,
			},
		}, payload.JSON),
	}

	// First add the service
	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Now update it
	updatedEntry := registry.Entry{
		ID:   entry.ID,
		Kind: entry.Kind,
		Data: payload.NewPayload(map[string]interface{}{
			"process": "test:updated-process",
			"host":    "updated-host",
			"input":   []interface{}{"updated-arg1", "updated-arg2"},
			"lifecycle": map[string]interface{}{
				"auto_start": false,
			},
		}, payload.JSON),
	}

	err = manager.Update(ctx, updatedEntry)
	require.NoError(t, err)

	// Verify the service still exists
	_, exists := manager.services.Load(entry.ID)
	assert.True(t, exists, "Service should still exist after update")
}

func TestManager_Add_WithoutTranscoder(t *testing.T) {
	// Create a logger that captures debug output
	logger := zap.NewNop()

	// Create event bus
	bus := eventbus.NewBus()

	// Create process manager
	procManager := &process.Manager{}

	// Create supervisor manager
	manager := NewSupervisorServiceManager(bus, procManager, logger)

	// Create a test context WITHOUT transcoder
	ctx := context.Background()

	// Create a test entry
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "test-service"},
		Kind: processapi.KindProcessService,
		Data: payload.NewPayload(map[string]interface{}{
			"process": "test:test-process",
			"host":    "test-host",
		}, payload.JSON),
	}

	// Test the Add method - should fail due to missing transcoder
	err := manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no transcoder found in context")
}

func TestManager_Add_InvalidKind(t *testing.T) {
	// Create a logger that captures debug output
	logger := zap.NewNop()

	// Create event bus
	bus := eventbus.NewBus()

	// Create process manager
	procManager := &process.Manager{}

	// Create supervisor manager
	manager := NewSupervisorServiceManager(bus, procManager, logger)

	// Create a test context with transcoder
	ctx := context.Background()
	ctx = payload.WithTranscoder(ctx, &MockTranscoder{})

	// Create a test entry with wrong kind
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "test-service"},
		Kind: "wrong.kind",
		Data: payload.NewPayload(map[string]interface{}{
			"process": "test:test-process",
			"host":    "test-host",
		}, payload.JSON),
	}

	// Test the Add method - should fail due to wrong kind
	err := manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

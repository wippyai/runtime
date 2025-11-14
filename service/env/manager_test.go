package env

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	serviceenv "github.com/wippyai/runtime/api/service/env"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// mockTranscoder implements payload.Transcoder for testing
type mockTranscoder struct{}

func (m *mockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	return payload.NewPayload(p, format), nil
}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	// Type switches based on expected output type and source payload
	switch dest := v.(type) {
	case *serviceenv.MemoryStorageConfig:
		if src, ok := p.Data().(*serviceenv.MemoryStorageConfig); ok {
			*dest = *src
			return nil
		}
	case *serviceenv.FileStorageConfig:
		if src, ok := p.Data().(*serviceenv.FileStorageConfig); ok {
			*dest = *src
			return nil
		}
	case *env.Variable:
		if src, ok := p.Data().(*env.Variable); ok {
			*dest = *src
			return nil
		}
	}

	return nil
}

func setupTestManager(_ *testing.T) (*Manager, *eventbus.Bus) {
	bus := eventbus.NewBus()
	logger := zap.NewNop()
	dtt := &mockTranscoder{}
	manager := NewManager(bus, dtt, logger, NewDefaultEnvStorageFactory())

	return manager, bus
}

func TestNewManager(t *testing.T) {
	bus := eventbus.NewBus()
	logger := zap.NewNop()
	dtt := &mockTranscoder{}

	manager := NewManager(bus, dtt, logger, NewDefaultEnvStorageFactory())

	assert.NotNil(t, manager)
	assert.Equal(t, bus, manager.bus)
	assert.Equal(t, dtt, manager.dtt)
	assert.Equal(t, logger, manager.logger)
	assert.NotNil(t, manager.storages)
	assert.NotNil(t, manager.factory)
}

func TestManager_Add_MemoryStorage(t *testing.T) {
	manager, _ := setupTestManager(t)

	entry := registry.Entry{
		Kind: serviceenv.KindStorageMemory,
		Data: payload.New(&serviceenv.MemoryStorageConfig{
			Meta: registry.Metadata{},
		}),
	}

	err := manager.Add(context.Background(), entry)
	require.NoError(t, err)

	// Verify storage was added
	manager.mu.RLock()
	storage, exists := manager.storages[entry.ID]
	manager.mu.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, storage)
}

package logs

import (
	"context"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	api "github.com/ponyruntime/pony/api/service/logs"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func setupManagerTest(t *testing.T) (*Manager, *eventbus.Bus) {
	t.Helper()
	bus := eventbus.NewBus()
	downstream := &testDownstreamCore{enabledResponse: true}
	logger := zap.NewNop()

	core := NewCore(downstream, bus)
	manager := NewManager(bus, core, logger)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = manager.Stop(ctx)
		bus.Stop()
	})

	return manager, bus
}

func TestManager_StartStop(t *testing.T) {
	manager, _ := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := manager.Start(ctx)
	require.NoError(t, err, "Manager should start without error")

	// Verify initial config
	cfg := manager.GetConfig()
	require.True(t, cfg.PropagateDownstream, "PropagateDownstream should be true by default")
	require.False(t, cfg.StreamToEvents, "StreamToEvents should be false by default")
	require.Equal(t, zapcore.InfoLevel, cfg.MinLevel, "Default level should be Info")

	err = manager.Stop(ctx)
	require.NoError(t, err, "Manager should stop without error")
}

func TestManager_InvalidConfigs(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	// Wait a bit for manager to initialize
	time.Sleep(100 * time.Millisecond)

	// Test sending invalid event data
	bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.SetConfigEvent,
		Path:   "test",
		Data:   "invalid", // Should be api.Config
	})

	// Give manager time to process (or ignore) invalid config
	time.Sleep(100 * time.Millisecond)

	// Original config should be preserved
	cfg := manager.GetConfig()
	require.True(t, cfg.PropagateDownstream, "Config should be unchanged after invalid update")
}

func TestManager_ConfigFlow(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	// Get initial config using helper
	initialConfig, err := GetConfig(ctx, bus)
	require.NoError(t, err)
	require.Equal(t, manager.GetConfig(), initialConfig)

	// Set new config using helper
	newConfig := api.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            zapcore.WarnLevel,
	}
	err = SetConfig(ctx, bus, newConfig)
	require.NoError(t, err)

	// Verify config was updated in manager
	managerConfig := manager.GetConfig()
	require.Equal(t, newConfig, managerConfig)

	// Get config again using helper
	updatedConfig, err := GetConfig(ctx, bus)
	require.NoError(t, err)
	require.Equal(t, newConfig, updatedConfig)
}

func TestManager_InvalidConfigChanges(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	tests := []struct {
		name      string
		sendEvent events.Event
	}{
		{
			name: "invalid data type",
			sendEvent: events.Event{
				System: api.System,
				Kind:   api.SetConfigEvent,
				Path:   "test",
				Data:   "invalid",
			},
		},
		{
			name: "nil data",
			sendEvent: events.Event{
				System: api.System,
				Kind:   api.SetConfigEvent,
				Path:   "test",
			},
		},
		{
			name: "wrong system",
			sendEvent: events.Event{
				System: "wrong.system",
				Kind:   api.SetConfigEvent,
				Path:   "test",
				Data:   api.Config{},
			},
		},
	}

	initialConfig := manager.GetConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.Send(ctx, tt.sendEvent)
			time.Sleep(50 * time.Millisecond) // Give manager time to process

			// Config should remain unchanged
			currentConfig := manager.GetConfig()
			require.Equal(t, initialConfig, currentConfig, "Config should remain unchanged after invalid update")
		})
	}
}

func TestManager_MultipleConfigUpdates(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	configs := []api.Config{
		{
			PropagateDownstream: false,
			StreamToEvents:      true,
			MinLevel:            zapcore.WarnLevel,
		},
		{
			PropagateDownstream: true,
			StreamToEvents:      true,
			MinLevel:            zapcore.ErrorLevel,
		},
		{
			PropagateDownstream: false,
			StreamToEvents:      false,
			MinLevel:            zapcore.DebugLevel,
		},
	}

	for i, cfg := range configs {
		err := SetConfig(ctx, bus, cfg)
		require.NoError(t, err, "Failed to set config %d", i)

		managerConfig := manager.GetConfig()
		require.Equal(t, cfg, managerConfig, "Manager config should match set config %d", i)

		fetchedConfig, err := GetConfig(ctx, bus)
		require.NoError(t, err, "Failed to get config %d", i)
		require.Equal(t, cfg, fetchedConfig, "Fetched config should match set config %d", i)
	}
}

func TestManager_StopBehavior(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	// Stop the manager
	require.NoError(t, manager.Stop(ctx))

	// Verify get/set operations fail after stop
	_, err := GetConfig(ctx, bus)
	require.Error(t, err, "GetConfig should fail after manager is stopped")

	err = SetConfig(ctx, bus, api.Config{})
	require.Error(t, err, "SetConfig should fail after manager is stopped")
}

package logs

import (
	"context"
	"testing"
	"time"

	api "github.com/wippyai/runtime/api/logs"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func setupManagerTest(t *testing.T) (*Manager, *eventbus.Bus) {
	t.Helper()
	bus := eventbus.NewBus()
	downstream := &testDownstreamCore{enabledResponse: true}
	logger := zap.NewNop()

	core := NewCore(downstream, bus)
	manager := NewManager(bus, core, logger, api.Config{
		PropagateDownstream: true,
		StreamToEvents:      true,
		MinLevel:            zapcore.InfoLevel,
	})

	t.Cleanup(func() {
		_ = manager.Stop()
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
	require.True(t, cfg.PropagateDownstream, "PropagateDownstream should be true")
	require.True(t, cfg.StreamToEvents, "StreamToEvents should be true")
	require.Equal(t, zapcore.InfoLevel, cfg.MinLevel, "Default level should be Info")

	err = manager.Stop()
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
	bus.Send(ctx, event.Event{
		System: api.System,
		Kind:   api.SetConfig,
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
	cfgm := NewConfigurationManager()
	// Spawn initial config using helper
	initialConfig, err := cfgm.GetConfig(ctx, bus)
	require.NoError(t, err)
	require.Equal(t, manager.GetConfig(), initialConfig)

	// Set new config using helper
	newConfig := api.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            zapcore.WarnLevel,
	}
	err = cfgm.SetConfig(ctx, bus, newConfig)
	require.NoError(t, err)

	// Verify config was updated in manager
	managerConfig := manager.GetConfig()
	require.Equal(t, newConfig, managerConfig)

	// Spawn config again using helper
	updatedConfig, err := cfgm.GetConfig(ctx, bus)
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
		sendEvent event.Event
	}{
		{
			name: "invalid data type",
			sendEvent: event.Event{
				System: api.System,
				Kind:   api.SetConfig,
				Path:   "test",
				Data:   "invalid",
			},
		},
		{
			name: "nil data",
			sendEvent: event.Event{
				System: api.System,
				Kind:   api.SetConfig,
				Path:   "test",
			},
		},
		{
			name: "wrong system",
			sendEvent: event.Event{
				System: "wrong.system",
				Kind:   api.SetConfig,
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

	cfgm := NewConfigurationManager()

	for i, cfg := range configs {
		err := cfgm.SetConfig(ctx, bus, cfg)
		require.NoError(t, err, "Failed to set config %d", i)

		managerConfig := manager.GetConfig()
		require.Equal(t, cfg, managerConfig, "Manager config should match set config %d", i)

		fetchedConfig, err := cfgm.GetConfig(ctx, bus)
		require.NoError(t, err, "Failed to get config %d", i)
		require.Equal(t, cfg, fetchedConfig, "Fetched config should match set config %d", i)
	}
}

func TestManager_StopBehavior(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	// close the manager
	require.NoError(t, manager.Stop())
	cfgm := NewConfigurationManager()

	// Verify get/set operations fail after stop
	_, err := cfgm.GetConfig(ctx, bus)
	require.Error(t, err, "GetConfig should fail after manager is stopped")

	err = cfgm.SetConfig(ctx, bus, api.Config{})
	require.Error(t, err, "SetConfig should fail after manager is stopped")
}

func TestManager_EventHandling(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	// Set initial config
	initialConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            0,
	}
	manager.handleConfigEvent(ctx, event.Event{
		System: api.System,
		Kind:   api.ConfigState,
		Data:   initialConfig,
	})

	t.Run("unknown_event_kind", func(_ *testing.T) {
		bus.Send(ctx, event.Event{
			System: api.System,
			Kind:   "unknown.kind",
			Path:   "test",
		})
		time.Sleep(50 * time.Millisecond) // Give manager time to process
	})

	t.Run("wrong_system_for_get_config", func(_ *testing.T) {
		bus.Send(ctx, event.Event{
			System: "wrong.system",
			Kind:   api.GetConfig,
			Path:   "test",
		})
		time.Sleep(50 * time.Millisecond) // Give manager time to process
	})

	t.Run("valid_get_config", func(t *testing.T) {
		// Create a channel to receive the response
		responseCh := make(chan event.Event, 1)
		subID, err := bus.SubscribeP(context.Background(), api.System, api.ConfigState, responseCh)
		require.NoError(t, err)
		defer bus.Unsubscribe(context.Background(), subID)

		// Send get config request
		bus.Send(ctx, event.Event{
			System: api.System,
			Kind:   api.GetConfig,
			Path:   "test",
		})

		// Wait for response
		select {
		case evt := <-responseCh:
			require.Equal(t, api.System, evt.System)
			require.Equal(t, api.ConfigState, evt.Kind)
			require.NotNil(t, evt.Data)
			config, ok := evt.Data.(api.Config)
			require.True(t, ok)
			require.Equal(t, initialConfig, config)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for config response")
		}
	})
}

func TestManager_ConfigValidation(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	tests := []struct {
		name      string
		config    interface{}
		expectErr bool
	}{
		{
			name:      "nil config",
			config:    nil,
			expectErr: true,
		},
		{
			name:      "invalid type",
			config:    "not a config",
			expectErr: true,
		},
		{
			name: "valid config",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      true,
				MinLevel:            zapcore.InfoLevel,
			},
			expectErr: false,
		},
		{
			name: "same config (no change)",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      false,
				MinLevel:            zapcore.InfoLevel,
			},
			expectErr: false,
		},
	}

	initialConfig := manager.GetConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.Send(ctx, event.Event{
				System: api.System,
				Kind:   api.SetConfig,
				Path:   "test",
				Data:   tt.config,
			})

			time.Sleep(50 * time.Millisecond) // Give manager time to process

			currentConfig := manager.GetConfig()
			if tt.expectErr {
				require.Equal(t, initialConfig, currentConfig, "Config should remain unchanged after invalid update")
			} else if tt.config != nil {
				cfg := tt.config.(api.Config)
				if cfg != initialConfig {
					require.Equal(t, cfg, currentConfig, "Config should be updated with valid config")
				} else {
					require.Equal(t, initialConfig, currentConfig, "Config should remain unchanged for same config")
				}
			}
		})
	}
}

func TestManager_GetConfigConcurrent(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	// Start multiple goroutines to get config concurrently
	const numGoroutines = 10
	done := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()

			// Send get config request
			bus.Send(ctx, event.Event{
				System: api.System,
				Kind:   api.GetConfig,
				Path:   "test",
			})

			// Also call GetConfig directly
			_ = manager.GetConfig()
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify manager is still in a valid state
	cfg := manager.GetConfig()
	require.NotNil(t, cfg, "Config should not be nil after concurrent access")
}

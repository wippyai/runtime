package logs

import (
	"context"
	"sync"
	"testing"
	"time"

	api "github.com/ponyruntime/pony/api/logs"

	"go.uber.org/zap/zaptest/observer"

	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func setupConfigSwitcherTest(t *testing.T) (*ConfigSwitcher, *Manager, *eventbus.Bus) {
	t.Helper()
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	// Spawn downstream core for the manager
	downstream := &testDownstreamCore{enabledResponse: true}
	core := NewCore(downstream, bus)

	// Spawn and start the manager
	manager := NewManager(bus, core, logger, api.Config{
		PropagateDownstream: true,
		StreamToEvents:      true,
		MinLevel:            zapcore.InfoLevel,
	})
	err := manager.Start(context.Background())
	require.NoError(t, err)

	switcher := NewConfigSwitcher(bus, logger)

	t.Cleanup(func() {
		_ = manager.Stop()
		bus.Stop()
	})

	return switcher, manager, bus
}

func TestConfigSwitcher_EnableTemporaryConfig(t *testing.T) {
	switcher, manager, _ := setupConfigSwitcherTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Set initial config via manager
	baseConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	// Switch to temporary config
	tempConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      true,
		MinLevel:            zapcore.DebugLevel,
	}
	err := switcher.EnableTemporaryConfig(ctx, tempConfig)
	require.NoError(t, err)

	// Verify the config was changed
	currentConfig := manager.GetConfig()
	require.Equal(t, tempConfig, currentConfig)

	// Verify base config was stored
	require.NotNil(t, switcher.baseConfig)
	require.Equal(t, baseConfig, *switcher.baseConfig)
}

func TestConfigSwitcher_RestoreBaseConfig(t *testing.T) {
	switcher, manager, _ := setupConfigSwitcherTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Set initial config via manager
	baseConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	// Enable temporary config and then restore
	tempConfig := api.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            zapcore.DebugLevel,
	}
	require.NoError(t, switcher.EnableTemporaryConfig(ctx, tempConfig))
	switcher.RestoreBaseConfig(ctx)

	// Verify config was restored
	currentConfig := manager.GetConfig()
	require.Equal(t, baseConfig, currentConfig)
}

func TestConfigSwitcher_Clear(t *testing.T) {
	switcher, manager, _ := setupConfigSwitcherTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Set base config via manager
	baseConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	tempConfig := api.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            zapcore.DebugLevel,
	}
	require.NoError(t, switcher.EnableTemporaryConfig(ctx, tempConfig))

	// Clear the stored config
	switcher.Clear()
	require.Nil(t, switcher.baseConfig)

	// Verify RestoreBaseConfig has no effect after Clear
	switcher.RestoreBaseConfig(ctx)
	currentConfig := manager.GetConfig()
	require.Equal(t, tempConfig, currentConfig)
}

func TestConfigSwitcher_EnableTemporaryConfigError(t *testing.T) {
	switcher, _, bus := setupConfigSwitcherTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// close the bus to force an error
	bus.Stop()

	// Attempt to enable temporary config
	tempConfig := api.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            zapcore.DebugLevel,
	}
	err := switcher.EnableTemporaryConfig(ctx, tempConfig)
	require.Error(t, err)
	require.Nil(t, switcher.baseConfig)
}

func TestConfigSwitcher_RestoreWithoutEnable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Set up a test observer for the logger
	core, observedLogs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	switcher := NewConfigSwitcher(&testEventBus{}, logger)

	// Attempt to restore without enabling first
	switcher.RestoreBaseConfig(ctx)

	// Verify no config changes were made
	require.Len(t, observedLogs.All(), 0)
}

func TestConfigSwitcher_MultipleTemporaryConfigs(t *testing.T) {
	switcher, manager, _ := setupConfigSwitcherTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Set initial config via manager
	baseConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	// Enable multiple temporary configs
	tempConfig1 := api.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            zapcore.DebugLevel,
	}
	tempConfig2 := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      true,
		MinLevel:            zapcore.WarnLevel,
	}

	require.NoError(t, switcher.EnableTemporaryConfig(ctx, tempConfig1))
	require.NoError(t, switcher.EnableTemporaryConfig(ctx, tempConfig2))

	// Verify only the initial config was stored
	require.NotNil(t, switcher.baseConfig)
	require.Equal(t, baseConfig, *switcher.baseConfig)

	// Restore and verify
	switcher.RestoreBaseConfig(ctx)
	currentConfig := manager.GetConfig()
	require.Equal(t, baseConfig, currentConfig)
}

func TestConfigSwitcher_RestoreBaseConfigError(t *testing.T) {
	switcher, manager, bus := setupConfigSwitcherTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Set initial config via manager
	baseConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	// Enable temporary config
	tempConfig := api.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            zapcore.DebugLevel,
	}
	require.NoError(t, switcher.EnableTemporaryConfig(ctx, tempConfig))

	// Stop the bus to force an error during restore
	bus.Stop()

	// Attempt to restore
	switcher.RestoreBaseConfig(ctx)

	// Verify config remains unchanged
	currentConfig := manager.GetConfig()
	require.Equal(t, tempConfig, currentConfig)
}

func TestConfigSwitcher_ConcurrentAccess(t *testing.T) {
	switcher, manager, _ := setupConfigSwitcherTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set initial config via manager
	baseConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	// Start multiple goroutines to switch configs concurrently
	const numGoroutines = 5
	done := make(chan struct{}, numGoroutines)
	successCount := 0
	var successMutex sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		go func(_ int) {
			defer func() { done <- struct{}{} }()

			tempConfig := api.Config{
				PropagateDownstream: false,
				StreamToEvents:      true,
				MinLevel:            zapcore.DebugLevel,
			}

			// Enable temporary config with timeout
			enableCtx, enableCancel := context.WithTimeout(ctx, 2*time.Second)
			err := switcher.EnableTemporaryConfig(enableCtx, tempConfig)
			enableCancel()

			if err == nil {
				successMutex.Lock()
				successCount++
				successMutex.Unlock()

				// Only restore if enable was successful
				restoreCtx, restoreCancel := context.WithTimeout(ctx, 2*time.Second)
				switcher.RestoreBaseConfig(restoreCtx)
				restoreCancel()
			}
		}(i)
	}

	// Wait for all goroutines to complete with timeout
	completed := 0
	for completed < numGoroutines {
		select {
		case <-done:
			completed++
		case <-ctx.Done():
			t.Fatal("Test timed out waiting for goroutines to complete")
		}
	}

	// Verify manager is still in a valid state
	cfg := manager.GetConfig()
	require.NotNil(t, cfg, "Config should not be nil after concurrent access")

	// With proper synchronization, all goroutines should succeed
	// since they will be serialized by the mutex
	require.Equal(t, numGoroutines, successCount, "All goroutines should have successfully enabled temp config")

	// Verify we're back to the base config
	require.Equal(t, baseConfig, cfg, "Config should be restored to base config")
}

func TestConfigSwitcher_ContextCancellation(t *testing.T) {
	switcher, manager, _ := setupConfigSwitcherTest(t)

	// Set initial config via manager
	baseConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(context.Background(), "test", baseConfig)

	tests := []struct {
		name      string
		operation func(context.Context) error
	}{
		{
			name: "EnableTemporaryConfig with canceled context",
			operation: func(ctx context.Context) error {
				return switcher.EnableTemporaryConfig(ctx, api.Config{})
			},
		},
		{
			name: "RestoreBaseConfig with canceled context",
			operation: func(ctx context.Context) error {
				switcher.RestoreBaseConfig(ctx)
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a context that's already canceled
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			// Perform the operation with canceled context
			err := tt.operation(ctx)
			if err != nil {
				require.ErrorIs(t, err, context.Canceled)
			}

			// Verify config remains unchanged
			currentConfig := manager.GetConfig()
			require.Equal(t, baseConfig, currentConfig)
		})
	}
}

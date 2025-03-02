package logs

import (
	"context"
	logsapi "github.com/ponyruntime/pony/api/logs"
	"testing"
	"time"

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
	manager := NewManager(bus, core, logger, zapcore.InfoLevel)
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
	baseConfig := logsapi.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	// Switch to temporary config
	tempConfig := logsapi.Config{
		PropagateDownstream: false,
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
	baseConfig := logsapi.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	// Enable temporary config and then restore
	tempConfig := logsapi.Config{
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
	baseConfig := logsapi.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	tempConfig := logsapi.Config{
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
	tempConfig := logsapi.Config{
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
	baseConfig := logsapi.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	// Enable multiple temporary configs
	tempConfig1 := logsapi.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            zapcore.DebugLevel,
	}
	tempConfig2 := logsapi.Config{
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

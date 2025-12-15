package logs

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	api "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// testConfigBus implements event.Bus for testing configuration operations
type testConfigBus struct {
	sendCalls []event.Event
	subs      map[event.System]map[event.Kind][]func(event.Event)
	mu        sync.RWMutex
}

func newTestConfigBus() *testConfigBus {
	return &testConfigBus{
		subs: make(map[event.System]map[event.Kind][]func(event.Event)),
	}
}

func (t *testConfigBus) Subscribe(_ context.Context, system event.System, ch chan<- event.Event) (event.SubscriberID, error) {
	t.subs[system][""] = append(t.subs[system][""], func(evt event.Event) {
		ch <- evt
	})
	return "test", nil
}

func (t *testConfigBus) SubscribeP(ctx context.Context, system event.System, kind event.Kind, ch chan<- event.Event) (event.SubscriberID, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.subs[system]; !ok {
		t.subs[system] = make(map[event.Kind][]func(event.Event))
	}
	if _, ok := t.subs[system][kind]; !ok {
		t.subs[system][kind] = make([]func(event.Event), 0)
	}

	handler := func(evt event.Event) {
		select {
		case ch <- evt:
		case <-ctx.Done():
		}
	}

	t.subs[system][kind] = append(t.subs[system][kind], handler)
	return "test", nil
}

func (t *testConfigBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

func (t *testConfigBus) Send(_ context.Context, evt event.Event) {
	t.mu.Lock()
	t.sendCalls = append(t.sendCalls, evt)
	t.mu.Unlock()

	t.mu.RLock()
	subs := t.subs[evt.System]
	handlers := subs[evt.Kind]
	t.mu.RUnlock()

	for _, handler := range handlers {
		handler(evt)
	}
}

func TestNewConfigurator(t *testing.T) {
	bus := newTestConfigBus()
	logger := zap.NewNop()
	cfg := NewConfigurator(bus, logger)
	if cfg == nil {
		t.Error("expected non-nil Configurator")
		return
	}
	if cfg.defaultTimeout != time.Second*20 {
		t.Errorf("expected default timeout of 20s, got %v", cfg.defaultTimeout)
	}
}

func TestConfigurator_GetConfig(t *testing.T) {
	tests := []struct {
		name          string
		config        api.Config
		timeout       time.Duration
		expectError   bool
		errorContains string
	}{
		{
			name: "successful config retrieval",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      true,
				MinLevel:            zapcore.InfoLevel,
			},
			timeout:     time.Second,
			expectError: false,
		},
		{
			name:          "timeout",
			timeout:       time.Millisecond * 10,
			expectError:   true,
			errorContains: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := newTestConfigBus()
			cfg := NewConfigurator(bus, zap.NewNop())
			cfg.defaultTimeout = tt.timeout

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout*2)
			defer cancel()

			done := make(chan struct{})
			go func() {
				result, err := cfg.GetConfig(ctx)
				if tt.expectError {
					if err == nil {
						t.Error("expected error, got nil")
					} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
						t.Errorf("error message does not contain %q: %v", tt.errorContains, err)
					}
				} else {
					if err != nil {
						t.Errorf("unexpected error: %v", err)
					}
					if result != tt.config {
						t.Errorf("expected config %v, got %v", tt.config, result)
					}
				}
				close(done)
			}()

			if !tt.expectError {
				time.Sleep(tt.timeout / 2)
				bus.Send(ctx, event.Event{
					System: api.System,
					Kind:   api.ConfigState,
					Path:   "get-logs-config-1",
					Data:   tt.config,
				})
			}

			<-done
		})
	}
}

func TestConfigurator_SetConfig(t *testing.T) {
	tests := []struct {
		name          string
		config        api.Config
		confirmConfig api.Config
		timeout       time.Duration
		expectError   bool
		errorContains string
	}{
		{
			name: "successful config set",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      true,
				MinLevel:            zapcore.InfoLevel,
			},
			confirmConfig: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      true,
				MinLevel:            zapcore.InfoLevel,
			},
			timeout:     time.Second,
			expectError: false,
		},
		{
			name: "config mismatch",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      true,
				MinLevel:            zapcore.InfoLevel,
			},
			confirmConfig: api.Config{
				PropagateDownstream: false,
				StreamToEvents:      true,
				MinLevel:            zapcore.InfoLevel,
			},
			timeout:       time.Second,
			expectError:   true,
			errorContains: "config mismatch",
		},
		{
			name:          "timeout",
			timeout:       time.Millisecond * 10,
			expectError:   true,
			errorContains: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := newTestConfigBus()
			cfg := NewConfigurator(bus, zap.NewNop())
			cfg.defaultTimeout = tt.timeout

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout*2)
			defer cancel()

			done := make(chan struct{})
			go func() {
				err := cfg.SetConfig(ctx, tt.config)
				if tt.expectError {
					if err == nil {
						t.Error("expected error, got nil")
					} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
						t.Errorf("error message does not contain %q: %v", tt.errorContains, err)
					}
				} else {
					if err != nil {
						t.Errorf("unexpected error: %v", err)
					}
				}
				close(done)
			}()

			if !tt.expectError || tt.errorContains == "config mismatch" {
				time.Sleep(tt.timeout / 2)
				bus.Send(ctx, event.Event{
					System: api.System,
					Kind:   api.ConfigState,
					Path:   "set-logs-config-1",
					Data:   tt.confirmConfig,
				})
			}

			<-done
		})
	}
}

func setupConfiguratorTest(t *testing.T) (*Configurator, *Manager, *eventbus.Bus) {
	t.Helper()
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	downstream := &testDownstreamCore{enabledResponse: true}
	core := NewCore(downstream, bus)

	manager := NewManager(bus, core, logger, api.Config{
		PropagateDownstream: true,
		StreamToEvents:      true,
		MinLevel:            zapcore.InfoLevel,
	})
	err := manager.Start(context.Background())
	require.NoError(t, err)

	configurator := NewConfigurator(bus, logger)

	t.Cleanup(func() {
		_ = manager.Stop()
		bus.Stop()
	})

	return configurator, manager, bus
}

func TestConfigurator_EnableTemporaryConfig(t *testing.T) {
	configurator, manager, _ := setupConfiguratorTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	baseConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

	tempConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      true,
		MinLevel:            zapcore.DebugLevel,
	}
	err := configurator.EnableTemporaryConfig(ctx, tempConfig)
	require.NoError(t, err)

	currentConfig := manager.GetConfig()
	require.Equal(t, tempConfig, currentConfig)

	require.NotNil(t, configurator.baseConfig)
	require.Equal(t, baseConfig, *configurator.baseConfig)
}

func TestConfigurator_RestoreBaseConfig(t *testing.T) {
	configurator, manager, _ := setupConfiguratorTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
	require.NoError(t, configurator.EnableTemporaryConfig(ctx, tempConfig))
	configurator.RestoreBaseConfig(ctx)

	currentConfig := manager.GetConfig()
	require.Equal(t, baseConfig, currentConfig)
}

func TestConfigurator_Clear(t *testing.T) {
	configurator, manager, _ := setupConfiguratorTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
	require.NoError(t, configurator.EnableTemporaryConfig(ctx, tempConfig))

	configurator.Clear()
	require.Nil(t, configurator.baseConfig)

	configurator.RestoreBaseConfig(ctx)
	currentConfig := manager.GetConfig()
	require.Equal(t, tempConfig, currentConfig)
}

func TestConfigurator_EnableTemporaryConfigError(t *testing.T) {
	configurator, _, bus := setupConfiguratorTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus.Stop()

	tempConfig := api.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            zapcore.DebugLevel,
	}
	err := configurator.EnableTemporaryConfig(ctx, tempConfig)
	require.Error(t, err)
	require.Nil(t, configurator.baseConfig)
}

func TestConfigurator_RestoreWithoutEnable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	core, observedLogs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	configurator := NewConfigurator(&testEventBus{}, logger)

	configurator.RestoreBaseConfig(ctx)

	require.Len(t, observedLogs.All(), 0)
}

func TestConfigurator_MultipleTemporaryConfigs(t *testing.T) {
	configurator, manager, _ := setupConfiguratorTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	baseConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

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

	require.NoError(t, configurator.EnableTemporaryConfig(ctx, tempConfig1))
	require.NoError(t, configurator.EnableTemporaryConfig(ctx, tempConfig2))

	require.NotNil(t, configurator.baseConfig)
	require.Equal(t, baseConfig, *configurator.baseConfig)

	configurator.RestoreBaseConfig(ctx)
	currentConfig := manager.GetConfig()
	require.Equal(t, baseConfig, currentConfig)
}

func TestConfigurator_RestoreBaseConfigError(t *testing.T) {
	configurator, manager, bus := setupConfiguratorTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
	require.NoError(t, configurator.EnableTemporaryConfig(ctx, tempConfig))

	bus.Stop()

	configurator.RestoreBaseConfig(ctx)

	currentConfig := manager.GetConfig()
	require.Equal(t, tempConfig, currentConfig)
}

func TestConfigurator_ConcurrentAccess(t *testing.T) {
	configurator, manager, _ := setupConfiguratorTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	baseConfig := api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.InfoLevel,
	}
	manager.handleSetConfigEvent(ctx, "test", baseConfig)

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

			enableCtx, enableCancel := context.WithTimeout(ctx, 2*time.Second)
			err := configurator.EnableTemporaryConfig(enableCtx, tempConfig)
			enableCancel()

			if err == nil {
				successMutex.Lock()
				successCount++
				successMutex.Unlock()

				restoreCtx, restoreCancel := context.WithTimeout(ctx, 2*time.Second)
				configurator.RestoreBaseConfig(restoreCtx)
				restoreCancel()
			}
		}(i)
	}

	completed := 0
	for completed < numGoroutines {
		select {
		case <-done:
			completed++
		case <-ctx.Done():
			t.Fatal("Test timed out waiting for goroutines to complete")
		}
	}

	cfg := manager.GetConfig()
	require.NotNil(t, cfg, "Config should not be nil after concurrent access")

	require.Equal(t, numGoroutines, successCount, "All goroutines should have successfully enabled temp config")

	require.Equal(t, baseConfig, cfg, "Config should be restored to base config")
}

func TestConfigurator_ContextCancellation(t *testing.T) {
	configurator, manager, _ := setupConfiguratorTest(t)

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
				return configurator.EnableTemporaryConfig(ctx, api.Config{})
			},
		},
		{
			name: "RestoreBaseConfig with canceled context",
			operation: func(ctx context.Context) error {
				configurator.RestoreBaseConfig(ctx)
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err := tt.operation(ctx)
			if err != nil {
				require.ErrorIs(t, err, context.Canceled)
			}

			currentConfig := manager.GetConfig()
			require.Equal(t, baseConfig, currentConfig)
		})
	}
}

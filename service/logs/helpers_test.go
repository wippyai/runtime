package logs

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	api "github.com/ponyruntime/pony/api/service/logs"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"sync"
	"testing"
	"time"
)

func setupTestBus(t *testing.T) *eventbus.Bus {
	t.Helper()
	bus := eventbus.NewBus()
	t.Cleanup(func() {
		// Give time for subscriptions to clean up before stopping
		time.Sleep(10 * time.Millisecond)
		bus.Stop()
	})
	return bus
}

func TestGetConfig(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(context.Context, *eventbus.Bus)
		wantConfig  api.Config
		wantErr     bool
		errContains string
	}{
		{
			name: "successful config retrieval",
			setup: func(ctx context.Context, bus *eventbus.Bus) {
				// Use a longer delay to ensure subscriber is ready
				time.Sleep(100 * time.Millisecond)
				bus.Send(ctx, events.Event{
					System: api.System,
					Kind:   api.ConfigStateEvent,
					Path:   "get-logs-config-1",
					Data: api.Config{
						PropagateDownstream: true,
						StreamToEvents:      false,
						MinLevel:            zapcore.InfoLevel,
					},
				})
			},
			wantConfig: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      false,
				MinLevel:            zapcore.InfoLevel,
			},
		},
		{
			name:        "timeout waiting for config",
			setup:       func(ctx context.Context, bus *eventbus.Bus) {}, // No response
			wantErr:     true,
			errContains: "timeout waiting for log config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := setupTestBus(t)

			// Use a reasonable timeout for the test context
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			// Setup in a separate goroutine
			if tt.setup != nil {
				go tt.setup(ctx, bus)
			}

			got, err := GetConfig(ctx, bus)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantConfig, got)
		})
	}
}

func TestSetConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      api.Config
		setup       func(context.Context, *eventbus.Bus, api.Config)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful config set",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      false,
				MinLevel:            zapcore.InfoLevel,
			},
			setup: func(ctx context.Context, bus *eventbus.Bus, cfg api.Config) {
				time.Sleep(100 * time.Millisecond)
				bus.Send(ctx, events.Event{
					System: api.System,
					Kind:   api.ConfigStateEvent,
					Path:   "set-logs-config-1",
					Data:   cfg,
				})
			},
		},
		{
			name: "config mismatch",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      false,
				MinLevel:            zapcore.InfoLevel,
			},
			setup: func(ctx context.Context, bus *eventbus.Bus, cfg api.Config) {
				time.Sleep(100 * time.Millisecond)
				differentCfg := api.Config{
					PropagateDownstream: false,
					StreamToEvents:      true,
					MinLevel:            zapcore.WarnLevel,
				}
				bus.Send(ctx, events.Event{
					System: api.System,
					Kind:   api.ConfigStateEvent,
					Path:   "set-logs-config-1",
					Data:   differentCfg,
				})
			},
			wantErr:     true,
			errContains: "config mismatch",
		},
		{
			name: "timeout waiting for confirmation",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      false,
				MinLevel:            zapcore.InfoLevel,
			},
			setup:       func(ctx context.Context, bus *eventbus.Bus, cfg api.Config) {}, // No response
			wantErr:     true,
			errContains: "timeout waiting for config confirmation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := setupTestBus(t)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			if tt.setup != nil {
				go tt.setup(ctx, bus, tt.config)
			}

			err := SetConfig(ctx, bus, tt.config)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestContextCancellation(t *testing.T) {
	t.Run("GetConfig with cancelled context", func(t *testing.T) {
		bus := setupTestBus(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := GetConfig(ctx, bus)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context canceled")
	})

	t.Run("SetConfig with cancelled context", func(t *testing.T) {
		bus := setupTestBus(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		cfg := api.Config{
			PropagateDownstream: true,
			StreamToEvents:      false,
			MinLevel:            zapcore.InfoLevel,
		}
		err := SetConfig(ctx, bus, cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context canceled")
	})
}

func TestInvalidResponses(t *testing.T) {
	t.Run("GetConfig with invalid response data", func(t *testing.T) {
		bus := setupTestBus(t)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		go func() {
			time.Sleep(100 * time.Millisecond)
			bus.Send(ctx, events.Event{
				System: api.System,
				Kind:   api.ConfigStateEvent,
				Path:   "get-logs-config-1",
				Data:   "invalid data", // Wrong type
			})
		}()

		_, err := GetConfig(ctx, bus)
		require.Error(t, err)
		require.Contains(t, err.Error(), "timeout waiting for log config")
	})

	t.Run("SetConfig with invalid response data", func(t *testing.T) {
		bus := setupTestBus(t)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		go func() {
			time.Sleep(100 * time.Millisecond)
			bus.Send(ctx, events.Event{
				System: api.System,
				Kind:   api.ConfigStateEvent,
				Path:   "set-logs-config-1",
				Data:   "invalid data", // Wrong type
			})
		}()

		cfg := api.Config{
			PropagateDownstream: true,
			StreamToEvents:      false,
			MinLevel:            zapcore.InfoLevel,
		}
		err := SetConfig(ctx, bus, cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "timeout waiting for config confirmation")
	})
}

func TestConcurrentConfigOperations(t *testing.T) {
	bus := setupTestBus(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// Run multiple get/set operations concurrently
	for i := 0; i < 5; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()
			cfg := api.Config{
				PropagateDownstream: true,
				StreamToEvents:      false,
				MinLevel:            zapcore.InfoLevel,
			}
			_ = SetConfig(ctx, bus, cfg)
		}()

		go func() {
			defer wg.Done()
			_, _ = GetConfig(ctx, bus)
		}()
	}

	// Wait for all operations to complete
	wg.Wait()
}

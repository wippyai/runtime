package logs

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/event"
	api "github.com/wippyai/runtime/api/logs"
	"go.uber.org/zap/zapcore"
)

// testConfigBus implements event.Bus for testing configuration operations
type testConfigBus struct {
	sendCalls []event.Event
	subs      map[event.System]map[event.Kind][]func(event.Event)
	mu        sync.RWMutex // protects sendCalls and subs
}

func newTestConfigBus() *testConfigBus {
	return &testConfigBus{
		subs: make(map[event.System]map[event.Kind][]func(event.Event)),
	}
}

func (t *testConfigBus) Subscribe(_ context.Context, system event.System, ch chan<- event.Event) (event.SubscriberID, error) {
	t.subs[system][event.Kind("")] = append(t.subs[system][event.Kind("")], func(evt event.Event) {
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

	// Create a handler that forwards events to the channel
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

func TestNewConfigurationManager(t *testing.T) {
	manager := NewConfigurationManager()
	if manager == nil {
		t.Error("expected non-nil ConfigurationManager")
		return
	}
	if manager.defaultTimeout != time.Second*20 {
		t.Errorf("expected default timeout of 20s, got %v", manager.defaultTimeout)
	}
}

func TestConfigurationManager_GetConfig(t *testing.T) {
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
			manager := NewConfigurationManager()
			manager.defaultTimeout = tt.timeout

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout*2)
			defer cancel()

			// Start the test in a goroutine to allow for async operations
			done := make(chan struct{})
			go func() {
				cfg, err := manager.GetConfig(ctx, bus)
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
					if cfg != tt.config {
						t.Errorf("expected config %v, got %v", tt.config, cfg)
					}
				}
				close(done)
			}()

			// Simulate config response if not testing timeout
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

func TestConfigurationManager_SetConfig(t *testing.T) {
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
			manager := NewConfigurationManager()
			manager.defaultTimeout = tt.timeout

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout*2)
			defer cancel()

			// Start the test in a goroutine to allow for async operations
			done := make(chan struct{})
			go func() {
				err := manager.SetConfig(ctx, bus, tt.config)
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

			// Simulate config response if not testing timeout
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

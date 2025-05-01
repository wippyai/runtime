// helpers.go

package logs

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	api "github.com/ponyruntime/pony/api/logs"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/system/eventbus"
)

// ConfigurationManager is a helper for managing logging configuration at runtime.
type ConfigurationManager struct {
	opCounter      atomic.Uint64
	defaultTimeout time.Duration
}

// NewConfigurationManager does not have a state except for the default timeout and the operation counter
func NewConfigurationManager() *ConfigurationManager {
	return &ConfigurationManager{
		defaultTimeout: time.Second * 20,
	}
}

// GetConfig requests and waits for logging configuration from the event bus
func (c *ConfigurationManager) GetConfig(ctx context.Context, bus event.Bus) (api.Config, error) {
	// Spawn a response channel with buffer to prevent blocking
	configCh := make(chan api.Config, 1)

	// Set up subscription first
	path := fmt.Sprintf("get-logs-config-%d", c.opCounter.Add(1))
	sub, err := eventbus.NewSubscriber(ctx, bus, api.System, api.ConfigState, func(e event.Event) {
		if e.Path == path {
			if cfg, ok := e.Data.(api.Config); ok {
				select {
				case configCh <- cfg:
				case <-ctx.Done():
				}
			}
		}
	})
	if err != nil {
		return api.Config{}, fmt.Errorf("failed to create subscriber: %w", err)
	}
	defer sub.Close()

	// Now send the request
	bus.Send(ctx, event.Event{
		System: api.System,
		Kind:   api.GetConfig,
		Path:   path,
	})

	// Wait for response with timeout
	select {
	case cfg := <-configCh:
		return cfg, nil
	case <-time.After(c.defaultTimeout):
		return api.Config{}, fmt.Errorf("timeout waiting for log config")
	case <-ctx.Done():
		return api.Config{}, fmt.Errorf("context canceled: %w", ctx.Err())
	}
}

// SetConfig sets logging configuration and waits for confirmation
func (c *ConfigurationManager) SetConfig(ctx context.Context, bus event.Bus, cfg api.Config) error {
	// Spawn a response channel with buffer to prevent blocking
	confirmCh := make(chan api.Config, 1)

	// Set up subscription first
	path := fmt.Sprintf("set-logs-config-%d", c.opCounter.Add(1))
	sub, err := eventbus.NewSubscriber(ctx, bus, api.System, api.ConfigState, func(e event.Event) {
		if e.Path == path {
			if confirm, ok := e.Data.(api.Config); ok {
				select {
				case confirmCh <- confirm:
				case <-ctx.Done():
				}
			}
		}
	})
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	defer sub.Close()

	// Now send the request
	bus.Send(ctx, event.Event{
		System: api.System,
		Kind:   api.SetConfig,
		Path:   path,
		Data:   cfg,
	})

	// Wait for response with timeout
	select {
	case confirm := <-confirmCh:
		if confirm != cfg {
			return fmt.Errorf("config mismatch - requested: %+v, got: %+v", cfg, confirm)
		}
		return nil
	case <-time.After(c.defaultTimeout):
		return fmt.Errorf("timeout waiting for config confirmation")
	case <-ctx.Done():
		return fmt.Errorf("context canceled: %w", ctx.Err())
	}
}

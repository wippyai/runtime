// helpers.go

package logs

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ponyruntime/pony/api/events"
	api "github.com/ponyruntime/pony/api/service/logs"
	"github.com/ponyruntime/pony/pkg/eventbus"
)

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
func (c *ConfigurationManager) GetConfig(ctx context.Context, bus events.Bus) (api.Config, error) {
	// Create a response channel with buffer to prevent blocking
	configCh := make(chan api.Config, 1)

	// Set up subscription first
	path := fmt.Sprintf("get-logs-config-%d", c.opCounter.Add(1))
	sub, err := eventbus.NewSubscriber(ctx, bus, api.System, api.ConfigStateEvent, func(e events.Event) {
		if string(e.Path) == path {
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
	bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.GetConfigEvent,
		Path:   events.Path(path),
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
func (c *ConfigurationManager) SetConfig(ctx context.Context, bus events.Bus, cfg api.Config) error {
	// Create a response channel with buffer to prevent blocking
	confirmCh := make(chan api.Config, 1)

	// Set up subscription first
	path := fmt.Sprintf("set-logs-config-%d", c.opCounter.Add(1))
	sub, err := eventbus.NewSubscriber(ctx, bus, api.System, api.ConfigStateEvent, func(e events.Event) {
		if string(e.Path) == path {
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
	bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.SetConfigEvent,
		Path:   events.Path(path),
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

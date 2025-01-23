package logs

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	api "github.com/ponyruntime/pony/api/service/logs"
	"github.com/ponyruntime/pony/pkg/eventbus"
)

// GetConfig requests and waits for logging configuration from the event bus
func GetConfig(ctx context.Context, bus events.Bus) (api.Config, error) {
	// Create temporary subscriber for config response
	configCh := make(chan api.Config, 1)
	sub, err := eventbus.NewSubscriber(ctx, bus, api.System, api.ConfigStateEvent, func(e events.Event) {
		if resp, ok := e.Data.(api.Config); ok {
			configCh <- resp
		}
	})
	if err != nil {
		return api.Config{}, fmt.Errorf("failed to create subscriber: %w", err)
	}
	defer sub.Close()

	// Send config request
	bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.GetConfigEvent,
		Path:   "get-logs-config",
	})

	// wait for response or timeout
	select {
	case cfg := <-configCh:
		return cfg, nil
	case <-ctx.Done():
		return api.Config{}, fmt.Errorf("timeout waiting for log config: %w", ctx.Err())
	}
}

// SetConfig sets logging configuration and waits for confirmation that it was applied
func SetConfig(ctx context.Context, bus events.Bus, cfg api.Config) error {
	confirmCh := make(chan api.Config, 1)

	// Subscribe to config state events to catch confirmation
	sub, err := eventbus.NewSubscriber(ctx, bus, api.System, api.ConfigStateEvent, func(e events.Event) {
		if resp, ok := e.Data.(api.Config); ok {
			confirmCh <- resp
		}
	})
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	defer sub.Close()

	// Send the config update
	bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.SetConfigEvent,
		Path:   "set-logs-config",
		Data:   cfg,
	})

	// wait for confirmation or timeout
	select {
	case confirm := <-confirmCh:
		if confirm != cfg {
			return fmt.Errorf("config mismatch - requested: %+v, got: %+v", cfg, confirm)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for config confirmation: %w", ctx.Err())
	}
}

// todo: add path verification and unique operation uuids

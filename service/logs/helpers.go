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
		if resp, ok := e.Data.(api.ConfigResponse); ok {
			configCh <- resp.Config
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

	// Wait for response or timeout
	select {
	case cfg := <-configCh:
		return cfg, nil
	case <-ctx.Done():
		return api.Config{}, fmt.Errorf("timeout waiting for log config: %w", ctx.Err())
	}
}

// helpers.go

package logs

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponyruntime/pony/api/events"
	api "github.com/ponyruntime/pony/api/service/logs"
	"github.com/ponyruntime/pony/pkg/eventbus"
)

const defaultTimeout = 20 * time.Second

var operationCounter atomic.Uint64

// GetConfig requests and waits for logging configuration from the event bus
func GetConfig(ctx context.Context, bus events.Bus) (api.Config, error) {
	// Create a WaitGroup to ensure proper cleanup
	var wg sync.WaitGroup

	// Create context with cancellation
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create channel with buffer to prevent blocking
	configCh := make(chan api.Config, 1)

	// Create error channel to propagate subscription errors
	errCh := make(chan error, 1)

	path := fmt.Sprintf("get-logs-config-%d", operationCounter.Add(1))

	wg.Add(1)
	go func() {
		defer wg.Done()

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
			select {
			case errCh <- fmt.Errorf("failed to create subscriber: %w", err):
			case <-ctx.Done():
			}
			return
		}

		// Wait for context cancellation
		<-ctx.Done()
		sub.Close()
	}()

	// Send config request
	bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.GetConfigEvent,
		Path:   events.Path(path),
	})

	// Wait for response or timeout
	select {
	case cfg := <-configCh:
		cancel()  // Cancel context to cleanup subscriber
		wg.Wait() // Wait for cleanup
		return cfg, nil
	case err := <-errCh:
		cancel()
		wg.Wait()
		return api.Config{}, err
	case <-time.After(defaultTimeout):
		cancel()
		wg.Wait()
		return api.Config{}, fmt.Errorf("timeout waiting for log config")
	case <-ctx.Done():
		wg.Wait()
		return api.Config{}, fmt.Errorf("context cancelled: %w", ctx.Err())
	}
}

// SetConfig sets logging configuration and waits for confirmation
func SetConfig(ctx context.Context, bus events.Bus, cfg api.Config) error {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	confirmCh := make(chan api.Config, 1)
	errCh := make(chan error, 1)

	path := fmt.Sprintf("set-logs-config-%d", operationCounter.Add(1))

	wg.Add(1)
	go func() {
		defer wg.Done()

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
			select {
			case errCh <- fmt.Errorf("failed to create subscriber: %w", err):
			case <-ctx.Done():
			}
			return
		}

		<-ctx.Done()
		sub.Close()
	}()

	// Send the config update
	bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.SetConfigEvent,
		Path:   events.Path(path),
		Data:   cfg,
	})

	select {
	case confirm := <-confirmCh:
		cancel()
		wg.Wait()
		if confirm != cfg {
			return fmt.Errorf("config mismatch - requested: %+v, got: %+v", cfg, confirm)
		}
		return nil
	case err := <-errCh:
		cancel()
		wg.Wait()
		return err
	case <-time.After(defaultTimeout):
		cancel()
		wg.Wait()
		return fmt.Errorf("timeout waiting for config confirmation")
	case <-ctx.Done():
		wg.Wait()
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	}
}

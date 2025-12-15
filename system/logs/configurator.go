package logs

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	api "github.com/wippyai/runtime/api/logs"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Configurator manages logging configuration via event bus.
// Provides get/set operations and temporary config switching with restore.
type Configurator struct {
	bus            event.Bus
	log            *zap.Logger
	baseConfig     *api.Config
	opCounter      atomic.Uint64
	defaultTimeout time.Duration
	mu             sync.Mutex
}

// NewConfigurator creates a new Configurator instance
func NewConfigurator(bus event.Bus, log *zap.Logger) *Configurator {
	return &Configurator{
		bus:            bus,
		log:            log,
		defaultTimeout: time.Second * 20,
	}
}

// GetConfig requests and waits for logging configuration from the event bus
func (c *Configurator) GetConfig(ctx context.Context) (api.Config, error) {
	configCh := make(chan api.Config, 1)

	path := fmt.Sprintf("get-logs-config-%d", c.opCounter.Add(1))
	sub, err := eventbus.NewSubscriber(ctx, c.bus, api.System, api.ConfigState, func(e event.Event) {
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
		return api.Config{}, NewSubscriberError(err)
	}
	defer sub.Close()

	c.bus.Send(ctx, event.Event{
		System: api.System,
		Kind:   api.GetConfig,
		Path:   path,
	})

	select {
	case cfg := <-configCh:
		return cfg, nil
	case <-time.After(c.defaultTimeout):
		return api.Config{}, ErrGetConfigTimeout
	case <-ctx.Done():
		return api.Config{}, NewContextCanceledError(ctx.Err())
	}
}

// SetConfig sets logging configuration and waits for confirmation
func (c *Configurator) SetConfig(ctx context.Context, cfg api.Config) error {
	confirmCh := make(chan api.Config, 1)

	path := fmt.Sprintf("set-logs-config-%d", c.opCounter.Add(1))
	sub, err := eventbus.NewSubscriber(ctx, c.bus, api.System, api.ConfigState, func(e event.Event) {
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
		return NewSubscriberError(err)
	}
	defer sub.Close()

	c.bus.Send(ctx, event.Event{
		System: api.System,
		Kind:   api.SetConfig,
		Path:   path,
		Data:   cfg,
	})

	select {
	case confirm := <-confirmCh:
		if confirm != cfg {
			return NewConfigMismatchError(fmt.Sprintf("%+v", cfg), fmt.Sprintf("%+v", confirm))
		}
		return nil
	case <-time.After(c.defaultTimeout):
		return ErrSetConfigTimeout
	case <-ctx.Done():
		return NewContextCanceledError(ctx.Err())
	}
}

// EnableTemporaryConfig switches to a temporary logging configuration while
// preserving the current config for later restoration
func (c *Configurator) EnableTemporaryConfig(ctx context.Context, tempConfig api.Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.baseConfig == nil {
		cfg, err := c.GetConfig(ctx)
		if err != nil {
			return NewGetLoggingConfigError(err)
		}
		c.baseConfig = &cfg
	}

	err := c.SetConfig(ctx, tempConfig)
	if err != nil {
		return NewSetTempConfigError(err)
	}

	c.log.Debug("temporary logging config enabled")
	return nil
}

// RestoreBaseConfig reverts to the original logging configuration
func (c *Configurator) RestoreBaseConfig(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.baseConfig != nil {
		if err := c.SetConfig(ctx, *c.baseConfig); err != nil {
			c.log.Error("failed to restore base logging config", zap.Error(err))
		} else {
			c.log.Debug("base logging config restored")
		}
	}
}

// Clear resets the stored base configuration
func (c *Configurator) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseConfig = nil
}

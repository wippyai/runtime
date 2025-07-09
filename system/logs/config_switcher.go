package logs

import (
	"context"
	"fmt"
	"sync"

	logsapi "github.com/ponyruntime/pony/api/logs"

	"github.com/ponyruntime/pony/api/event"
	"go.uber.org/zap"
)

// ConfigSwitcher manages switching between different logging configurations
// while preserving the ability to restore the original configuration
type ConfigSwitcher struct {
	bus        event.Bus
	log        *zap.Logger
	baseConfig *logsapi.Config
	cfgManager *ConfigurationManager
	mu         sync.Mutex // Protect baseConfig access
}

// NewConfigSwitcher creates a new ConfigSwitcher instance
func NewConfigSwitcher(bus event.Bus, log *zap.Logger) *ConfigSwitcher {
	return &ConfigSwitcher{
		bus:        bus,
		log:        log,
		cfgManager: NewConfigurationManager(),
	}
}

// EnableTemporaryConfig switches to a temporary logging configuration while
// preserving the current config for later restoration
func (c *ConfigSwitcher) EnableTemporaryConfig(ctx context.Context, tempConfig logsapi.Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Only store base config on the first switch
	if c.baseConfig == nil {
		// Spawn current config
		cfg, err := c.cfgManager.GetConfig(ctx, c.bus)
		if err != nil {
			return fmt.Errorf("failed to get logging config: %w", err)
		}
		c.baseConfig = &cfg
	}

	// Apply temporary config
	err := c.cfgManager.SetConfig(ctx, c.bus, tempConfig)
	if err != nil {
		return fmt.Errorf("failed to set temporary config: %w", err)
	}

	c.log.Debug("temporary logging config enabled")
	return nil
}

// RestoreBaseConfig reverts to the original logging configuration
func (c *ConfigSwitcher) RestoreBaseConfig(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.baseConfig != nil {
		if err := c.cfgManager.SetConfig(ctx, c.bus, *c.baseConfig); err != nil {
			c.log.Error("failed to restore base logging config", zap.Error(err))
		} else {
			c.log.Debug("base logging config restored")
		}
	}
}

// Clear resets the stored base configuration
func (c *ConfigSwitcher) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseConfig = nil
}

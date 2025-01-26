package logs

import (
	"context"
	"fmt"
	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/events"
	logsapi "github.com/ponyruntime/pony/api/service/logs"
)

// LogSwitcher manages terminal logging configuration
type LogSwitcher struct {
	bus        events.Bus
	log        *zap.Logger
	baseConfig *logsapi.Config
}

func NewLogSwitcher(bus events.Bus, log *zap.Logger) *LogSwitcher {
	return &LogSwitcher{
		bus: bus,
		log: log,
	}
}

// EnableOn switches to terminal-specific logging configuration
func (l *LogSwitcher) EnableOn(ctx context.Context) error {
	// Get current config
	cfg, err := GetConfig(ctx, l.bus)
	if err != nil {
		return fmt.Errorf("failed to get logging config: %w", err)
	}
	l.baseConfig = &cfg

	// Apply terminal logging config
	err = SetConfig(ctx, l.bus, logsapi.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            l.baseConfig.MinLevel,
	})
	if err != nil {
		return fmt.Errorf("failed to set terminal logging config: %w", err)
	}

	l.log.Debug("terminal logging enabled")
	return nil
}

// RestoreOn reverts to original logging configuration
func (l *LogSwitcher) RestoreOn(ctx context.Context) {
	if l.baseConfig != nil {
		if err := SetConfig(ctx, l.bus, *l.baseConfig); err != nil {
			l.log.Error("failed to RestoreOn logging config", zap.Error(err))
		} else {
			l.log.Debug("logging config restored")
		}
	}
}

// Clear resets the stored base configuration
func (l *LogSwitcher) Clear() {
	l.baseConfig = nil
}

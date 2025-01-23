package terminal

import (
	"context"
	"fmt"
	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/events"
	logsapi "github.com/ponyruntime/pony/api/service/logs"
	"github.com/ponyruntime/pony/service/logs"
)

// logSwitcher manages terminal logging configuration
type logSwitcher struct {
	bus        events.Bus
	log        *zap.Logger
	baseConfig *logsapi.Config
}

func newLogSwitcher(bus events.Bus, log *zap.Logger) *logSwitcher {
	return &logSwitcher{
		bus: bus,
		log: log,
	}
}

// enable switches to terminal-specific logging configuration
func (l *logSwitcher) enable(ctx context.Context) error {
	// Get current config
	cfg, err := logs.GetConfig(ctx, l.bus)
	if err != nil {
		return fmt.Errorf("failed to get logging config: %w", err)
	}
	l.baseConfig = &cfg

	// Apply terminal logging config
	err = logs.SetConfig(ctx, l.bus, logsapi.Config{
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

// restore reverts to original logging configuration
func (l *logSwitcher) restore(ctx context.Context) {
	if l.baseConfig != nil {
		if err := logs.SetConfig(ctx, l.bus, *l.baseConfig); err != nil {
			l.log.Error("failed to restore logging config", zap.Error(err))
		} else {
			l.log.Debug("logging config restored")
		}
	}
}

// clear resets the stored base configuration
func (l *logSwitcher) clear() {
	l.baseConfig = nil
}

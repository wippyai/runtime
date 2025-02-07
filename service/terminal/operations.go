package terminal

import (
	"context"
	"github.com/ponyruntime/pony/pkg/logs"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/terminal"
	"go.uber.org/zap"
)

type controlAction int

const (
	actionStart controlAction = iota
	actionStop
	actionUpdate
)

type controlOp struct {
	action   controlAction
	terminal api.Terminal
	id       registry.ID
	result   chan error
}

type operations struct {
	terminal *terminalRunner
	bus      events.Bus
	log      *zap.Logger
	csw      *logs.ConfigSwitcher
	statusCh chan<- any // Write-only channel
}

func newOperations(
	terminal *terminalRunner,
	bus events.Bus,
	log *zap.Logger,
	csw *logs.ConfigSwitcher,
	statusCh chan<- any,
) *operations {
	return &operations{
		terminal: terminal,
		bus:      bus,
		log:      log,
		csw:      csw,
		statusCh: statusCh,
	}
}

func (o *operations) handleStart(ctx context.Context) error {
	if err := o.terminal.start(ctx); err != nil {
		return err
	}

	o.sendStatus("running")
	return nil
}

func (o *operations) handleStop(ctx context.Context) error {
	if err := o.terminal.stop(ctx); err != nil {
		return err
	}

	o.sendStatus("stopped")
	return nil
}

func (o *operations) handleUpdate(ctx context.Context, newTerminal api.Terminal, id registry.ID) error {
	newRunner := newTerminalRunner(newTerminal, id, o.bus, o.log)

	// todo: introduce context and timeout

	if err := o.terminal.stop(ctx); err != nil {
		return err
	}

	if err := o.terminal.transferState(newRunner); err != nil {
		return err
	}

	if err := newRunner.start(ctx); err != nil {
		return err
	}

	o.terminal = newRunner
	o.sendStatus("terminal updated")
	return nil
}

// sendStatus attempts to send status update, non-blocking
func (o *operations) sendStatus(status any) {
	// Use non-blocking send to prevent deadlocks
	select {
	case o.statusCh <- status:
	default:
		o.log.Warn("failed to send status update", zap.Any("status", status))
	}
}

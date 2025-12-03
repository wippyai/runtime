// Package clock provides time-related command handlers for the dispatcher system.
// Uses timing wheel for efficient timer management with callback-based async execution.
package clock

import (
	"context"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
)

// Dispatcher handles clock commands using timing wheel for async operations.
type Dispatcher struct {
	wheel   *WheelTimerRegistry
	tickers *TickerRegistry
}

// NewDispatcher creates a clock dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		wheel:   NewWheelTimerRegistry(),
		tickers: NewTickerRegistry(),
	}
}

// Start is a no-op - timing wheel starts automatically.
func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

// Stop shuts down the timing wheel and ticker registry.
func (d *Dispatcher) Stop(_ context.Context) error {
	if d.wheel != nil {
		d.wheel.Close()
	}
	if d.tickers != nil {
		d.tickers.Close()
	}
	return nil
}

// RegisterAll registers all clock handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(clockapi.CmdSleep, dispatcher.HandlerFunc(d.handleSleep))
	register(clockapi.CmdNow, dispatcher.HandlerFunc(d.handleNow))
	register(clockapi.CmdAfter, dispatcher.HandlerFunc(d.handleAfter))
	register(clockapi.CmdTickerStart, dispatcher.HandlerFunc(d.handleTickerStart))
	register(clockapi.CmdTickerNext, dispatcher.HandlerFunc(d.handleTickerNext))
	register(clockapi.CmdTickerStop, dispatcher.HandlerFunc(d.handleTickerStop))
	register(clockapi.CmdTimerStart, dispatcher.HandlerFunc(d.handleTimerStart))
	register(clockapi.CmdTimerWait, dispatcher.HandlerFunc(d.handleTimerWait))
	register(clockapi.CmdTimerStop, dispatcher.HandlerFunc(d.handleTimerStop))
	register(clockapi.CmdTimerReset, dispatcher.HandlerFunc(d.handleTimerReset))
}

// shortSleepThreshold is the cutoff for using time.AfterFunc vs timing wheel.
// Short sleeps use Go's optimized timer heap directly.
const shortSleepThreshold = 10 * time.Millisecond

func (d *Dispatcher) handleSleep(_ context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	c := cmd.(clockapi.SleepCmd)
	if c.Duration <= 0 {
		emit.Emit(nil, nil)
		return nil
	}
	if c.Duration < shortSleepThreshold {
		time.AfterFunc(c.Duration, func() {
			emit.Emit(nil, nil)
		})
		return nil
	}
	d.wheel.StartWithCallback(c.Duration, func() {
		emit.Emit(nil, nil)
	})
	return nil
}

func (d *Dispatcher) handleNow(ctx context.Context, _ dispatcher.Command, emit dispatcher.Emitter) error {
	if ref := clockapi.GetTimeReference(ctx); ref != nil {
		emit.Emit(ref.Now().UnixNano(), nil)
		return nil
	}
	emit.Emit(time.Now().UnixNano(), nil)
	return nil
}

func (d *Dispatcher) handleAfter(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	c := cmd.(clockapi.AfterCmd)
	if c.Duration <= 0 {
		return nil
	}
	registry := GetOrCreateAfterRegistry(ctx)
	id := registry.Create(ctx, c.Duration)
	emit.Emit(&AfterResult{ChannelID: id}, nil)
	return nil
}

func (d *Dispatcher) handleTickerStart(_ context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	c := cmd.(clockapi.TickerStartCmd)
	if c.Duration <= 0 {
		return nil
	}
	id := d.tickers.Start(c.Duration)
	emit.Emit(id, nil)
	return nil
}

func (d *Dispatcher) handleTickerNext(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	c := cmd.(clockapi.TickerNextCmd)
	go func() {
		t, err := d.tickers.Next(ctx, c.TickerID)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			emit.Emit(nil, err)
			return
		}
		emit.Emit(t.UnixNano(), nil)
	}()
	return nil
}

func (d *Dispatcher) handleTickerStop(_ context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	c := cmd.(clockapi.TickerStopCmd)
	if err := d.tickers.Stop(c.TickerID); err != nil {
		emit.Emit(nil, err)
		return nil
	}
	emit.Emit(nil, nil)
	return nil
}

func (d *Dispatcher) handleTimerStart(_ context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	c := cmd.(clockapi.TimerStartCmd)
	if c.Duration <= 0 {
		return nil
	}
	id := d.wheel.Start(c.Duration)
	emit.Emit(id, nil)
	return nil
}

func (d *Dispatcher) handleTimerWait(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	c := cmd.(clockapi.TimerWaitCmd)
	go func() {
		t, err := d.wheel.Wait(ctx, c.TimerID)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			emit.Emit(nil, err)
			return
		}
		emit.Emit(t.UnixNano(), nil)
	}()
	return nil
}

func (d *Dispatcher) handleTimerStop(_ context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	c := cmd.(clockapi.TimerStopCmd)
	stopped, err := d.wheel.Stop(c.TimerID)
	if err != nil {
		emit.Emit(nil, err)
		return nil
	}
	emit.Emit(stopped, nil)
	return nil
}

func (d *Dispatcher) handleTimerReset(_ context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	c := cmd.(clockapi.TimerResetCmd)
	if c.Duration <= 0 {
		return nil
	}
	wasActive, err := d.wheel.Reset(c.TimerID, c.Duration)
	if err != nil {
		emit.Emit(nil, err)
		return nil
	}
	emit.Emit(wasActive, nil)
	return nil
}

// AfterResult contains the channel ID for time.after().
type AfterResult struct {
	ChannelID uint64
}

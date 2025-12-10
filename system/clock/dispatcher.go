// Package clock provides time-related command handlers for the dispatcher system.
// Uses timing wheel for efficient timer management with callback-based async execution.
package clock

import (
	"context"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
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
	register(clockapi.Sleep, dispatcher.HandlerFunc(d.handleSleep))
	register(clockapi.TickerStart, dispatcher.HandlerFunc(d.handleTickerStart))
	register(clockapi.TickerStop, dispatcher.HandlerFunc(d.handleTickerStop))
	register(clockapi.TimerStart, dispatcher.HandlerFunc(d.handleTimerStart))
	register(clockapi.TimerWait, dispatcher.HandlerFunc(d.handleTimerWait))
	register(clockapi.TimerStop, dispatcher.HandlerFunc(d.handleTimerStop))
	register(clockapi.TimerReset, dispatcher.HandlerFunc(d.handleTimerReset))
}

// shortSleepThreshold is the cutoff for using time.AfterFunc vs timing wheel.
// Short sleeps use Go's optimized timer heap directly.
const shortSleepThreshold = 10 * time.Millisecond

func (d *Dispatcher) handleSleep(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.SleepCmd)
	if c.Duration <= 0 {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}
	if c.Duration < shortSleepThreshold {
		time.AfterFunc(c.Duration, func() {
			receiver.CompleteYield(tag, nil, nil)
		})
		return nil
	}
	d.wheel.StartWithCallback(c.Duration, func() {
		receiver.CompleteYield(tag, nil, nil)
	})
	return nil
}

func (d *Dispatcher) handleTickerStart(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TickerStartCmd)
	if c.Duration <= 0 {
		return nil
	}

	node := relay.GetNode(ctx)
	if node == nil {
		return nil
	}

	id := d.tickers.Start(ctx, c.Duration, c.PID, c.Topic, node)
	receiver.CompleteYield(tag, clockapi.TickerStartResult{
		ID: id,
		Stop: func() {
			_ = d.tickers.Stop(id)
		},
	}, nil)
	return nil
}

func (d *Dispatcher) handleTickerStop(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TickerStopCmd)
	if err := d.tickers.Stop(c.TickerID); err != nil {
		receiver.CompleteYield(tag, nil, err)
		return nil
	}
	receiver.CompleteYield(tag, nil, nil)
	return nil
}

func (d *Dispatcher) handleTimerStart(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerStartCmd)
	if c.Duration <= 0 {
		return nil
	}

	node := relay.GetNode(ctx)
	if node == nil {
		return nil
	}

	// Start timer with callback that sends to topic when it fires
	id := d.wheel.StartWithCallback(c.Duration, func() {
		t := time.Now()
		p := payload.NewPayload(t.UnixNano(), payload.Golang)
		pkg := relay.NewPackage(relay.PID{}, c.PID, c.Topic, p)
		_ = node.Send(pkg)
	})

	receiver.CompleteYield(tag, clockapi.TimerStartResult{
		ID: id,
		Stop: func() {
			_, _ = d.wheel.Stop(id)
		},
	}, nil)
	return nil
}

func (d *Dispatcher) handleTimerWait(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerWaitCmd)
	go func() {
		t, err := d.wheel.Wait(ctx, c.TimerID)
		if ctx.Err() != nil {
			receiver.CompleteYield(tag, nil, ctx.Err())
			return
		}
		if err != nil {
			receiver.CompleteYield(tag, nil, err)
			return
		}
		receiver.CompleteYield(tag, t.UnixNano(), nil)
	}()
	return nil
}

func (d *Dispatcher) handleTimerStop(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerStopCmd)
	stopped, err := d.wheel.Stop(c.TimerID)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return nil
	}
	receiver.CompleteYield(tag, stopped, nil)
	return nil
}

func (d *Dispatcher) handleTimerReset(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerResetCmd)
	if c.Duration <= 0 {
		return nil
	}
	wasActive, err := d.wheel.Reset(c.TimerID, c.Duration)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return nil
	}
	receiver.CompleteYield(tag, wasActive, nil)
	return nil
}

// TickerCount returns the number of active tickers.
func (d *Dispatcher) TickerCount() int {
	return d.tickers.Count()
}

// TimerCount returns the number of active timers.
func (d *Dispatcher) TimerCount() int {
	return d.wheel.Count()
}

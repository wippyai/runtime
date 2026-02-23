// SPDX-License-Identifier: MPL-2.0

// Package clock provides time-related command handlers for the dispatcher system.
package clock

import (
	"context"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// Dispatcher handles clock commands.
type Dispatcher struct {
	timers  *timerRegistry
	tickers *tickerRegistry
}

func shouldIgnoreDuration(d time.Duration) bool {
	return d <= 0
}

// NewDispatcher creates a clock dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		timers:  newTimerRegistry(),
		tickers: newTickerRegistry(),
	}
}

// Start is a no-op.
func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

// Stop shuts down timers and tickers.
func (d *Dispatcher) Stop(_ context.Context) error {
	if d.timers != nil {
		d.timers.close()
	}
	if d.tickers != nil {
		d.tickers.close()
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

func (d *Dispatcher) handleSleep(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.SleepCmd)
	if shouldIgnoreDuration(c.Duration) {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}
	time.AfterFunc(c.Duration, func() {
		receiver.CompleteYield(tag, nil, nil)
	})
	return nil
}

func (d *Dispatcher) handleTickerStart(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TickerStartCmd)
	if shouldIgnoreDuration(c.Duration) {
		return nil
	}

	node := relay.GetNode(ctx)
	if node == nil {
		return nil
	}

	id := d.tickers.start(ctx, c.Duration, c.PID, c.Topic, node)
	receiver.CompleteYield(tag, clockapi.TickerStartResult{
		ID: id,
		Stop: func() {
			_ = d.tickers.stop(id)
		},
	}, nil)
	return nil
}

func (d *Dispatcher) handleTickerStop(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TickerStopCmd)
	if err := d.tickers.stop(c.TickerID); err != nil {
		receiver.CompleteYield(tag, nil, err)
		return nil
	}
	receiver.CompleteYield(tag, nil, nil)
	return nil
}

func (d *Dispatcher) handleTimerStart(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerStartCmd)
	if shouldIgnoreDuration(c.Duration) {
		return nil
	}

	node := relay.GetNode(ctx)
	if node == nil {
		return nil
	}

	id := d.timers.startWithCallback(c.Duration, func() {
		sendTick(node, c.PID, c.Topic, time.Now())
	})

	receiver.CompleteYield(tag, clockapi.TimerStartResult{
		ID: id,
		Stop: func() {
			_, _ = d.timers.stop(id)
		},
	}, nil)
	return nil
}

func (d *Dispatcher) handleTimerWait(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerWaitCmd)
	go func() {
		t, err := d.timers.wait(ctx, c.TimerID)
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
	stopped, err := d.timers.stop(c.TimerID)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return nil
	}
	receiver.CompleteYield(tag, stopped, nil)
	return nil
}

func (d *Dispatcher) handleTimerReset(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerResetCmd)
	if shouldIgnoreDuration(c.Duration) {
		return nil
	}
	wasActive, err := d.timers.reset(c.TimerID, c.Duration)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return nil
	}
	receiver.CompleteYield(tag, wasActive, nil)
	return nil
}

func sendTick(node relay.Node, target pid.PID, topic string, at time.Time) {
	p := payload.NewPayload(at.UnixNano(), payload.Golang)
	pkg := relay.NewPackage(pid.PID{}, target, topic, p)
	_ = node.Send(pkg)
}

// TickerCount returns the number of active tickers.
func (d *Dispatcher) TickerCount() int {
	return d.tickers.count()
}

// TimerCount returns the number of active timers.
func (d *Dispatcher) TimerCount() int {
	return d.timers.count()
}

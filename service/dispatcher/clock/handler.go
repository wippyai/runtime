// Package clock provides time-related command handlers for the dispatcher system.
// Handlers follow the Go-idiomatic pattern: context for cancellation, emit for values, return for completion.
package clock

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	clockapi "github.com/wippyai/runtime/api/dispatcher/clock"
	"github.com/wippyai/runtime/api/workflow"
)

// SleepHandler processes sleep commands.
// One-shot: blocks for duration, then returns (no emit calls).
type SleepHandler struct{}

// NewSleepHandler creates a new sleep handler.
func NewSleepHandler() *SleepHandler {
	return &SleepHandler{}
}

// Handle implements dispatcher.Handler.
// Blocks until duration elapses or context is cancelled.
func (h *SleepHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	sleep := cmd.(clockapi.SleepCmd)

	if sleep.Duration <= 0 {
		return nil
	}

	timer := time.NewTimer(sleep.Duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// TickerHandler processes ticker commands.
// Streaming: emits tick values at regular intervals until cancelled.
type TickerHandler struct{}

// NewTickerHandler creates a new ticker handler.
func NewTickerHandler() *TickerHandler {
	return &TickerHandler{}
}

// Handle implements dispatcher.Handler.
// Emits current time on each tick until context is cancelled.
func (h *TickerHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	ticker := cmd.(clockapi.TickerCmd)

	if ticker.Duration <= 0 {
		return nil
	}

	t := time.NewTicker(ticker.Duration)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tick := <-t.C:
			emit(tick)
		}
	}
}

// TimerHandler processes one-shot timer commands.
// Emits the fire time once, then returns.
type TimerHandler struct{}

// NewTimerHandler creates a new timer handler.
func NewTimerHandler() *TimerHandler {
	return &TimerHandler{}
}

// Handle implements dispatcher.Handler.
// Waits for duration, emits fire time, returns.
func (h *TimerHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	timer := cmd.(clockapi.TimerCmd)

	if timer.Duration <= 0 {
		emit(now(ctx))
		return nil
	}

	t := time.NewTimer(timer.Duration)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case fireTime := <-t.C:
		emit(fireTime)
		return nil
	}
}

// Service bundles all clock handlers for convenient registration.
type Service struct {
	Sleep  *SleepHandler
	Ticker *TickerHandler // legacy streaming ticker
	Timer  *TimerHandler
	Now    *NowHandler

	// Streaming ticker handlers (decomposed one-shot)
	TickerStart *TickerStartHandler
	TickerNext  *TickerNextHandler
	TickerStop  *TickerStopHandler
}

// NewService creates a new clock service with all handlers initialized.
func NewService() *Service {
	return &Service{
		Sleep:       NewSleepHandler(),
		Ticker:      NewTickerHandler(),
		Timer:       NewTimerHandler(),
		Now:         NewNowHandler(),
		TickerStart: NewTickerStartHandler(),
		TickerNext:  NewTickerNextHandler(),
		TickerStop:  NewTickerStopHandler(),
	}
}

// RegisterAll registers all clock handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(clockapi.CmdSleep, s.Sleep)
	register(clockapi.CmdTicker, s.Ticker)
	register(clockapi.CmdTimer, s.Timer)
	register(clockapi.CmdNow, s.Now)
	register(clockapi.CmdTickerStart, s.TickerStart)
	register(clockapi.CmdTickerNext, s.TickerNext)
	register(clockapi.CmdTickerStop, s.TickerStop)
}

// NowHandler returns current time.
// Uses TimeReference from context if available (for deterministic workflow time).
type NowHandler struct{}

// NewNowHandler creates a new now handler.
func NewNowHandler() *NowHandler {
	return &NowHandler{}
}

// Handle implements dispatcher.Handler.
// Emits current time as nanoseconds (int64) for Lua compatibility.
func (h *NowHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	emit(now(ctx).UnixNano())
	return nil
}

// now returns current time from TimeReference if available, else system time.
func now(ctx context.Context) time.Time {
	if ref := workflow.GetTimeReference(ctx); ref != nil {
		return ref.Now()
	}
	return time.Now()
}

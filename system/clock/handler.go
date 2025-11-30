// Package clock provides time-related command handlers for the dispatcher system.
// Handlers follow the Go-idiomatic pattern: context for cancellation, emit for values, return for completion.
package clock

import (
	"context"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
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

// AfterHandler creates a channel that fires after a duration.
// Returns immediately with channel reference, fires in background.
type AfterHandler struct{}

func NewAfterHandler() *AfterHandler {
	return &AfterHandler{}
}

// AfterResult contains the channel and cleanup info for time.after().
type AfterResult struct {
	ChannelID uint64
}

func (h *AfterHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	afterCmd := cmd.(clockapi.AfterCmd)

	if afterCmd.Duration <= 0 {
		return nil
	}

	registry := GetOrCreateAfterRegistry(ctx)
	id := registry.Create(ctx, afterCmd.Duration)

	emit(&AfterResult{ChannelID: id})
	return nil
}

// Service bundles all clock handlers for convenient registration.
type Service struct {
	Sleep *SleepHandler
	Now   *NowHandler
	After *AfterHandler

	// Decomposed ticker handlers (one-shot pattern)
	TickerStart *TickerStartHandler
	TickerNext  *TickerNextHandler
	TickerStop  *TickerStopHandler

	// Decomposed timer handlers (one-shot pattern)
	TimerStart *TimerStartHandler
	TimerWait  *TimerWaitHandler
	TimerStop  *TimerStopHandler
	TimerReset *TimerResetHandler
}

// NewService creates a new clock service with all handlers initialized.
func NewService() *Service {
	return &Service{
		Sleep:       NewSleepHandler(),
		Now:         NewNowHandler(),
		After:       NewAfterHandler(),
		TickerStart: NewTickerStartHandler(),
		TickerNext:  NewTickerNextHandler(),
		TickerStop:  NewTickerStopHandler(),
		TimerStart:  NewTimerStartHandler(),
		TimerWait:   NewTimerWaitHandler(),
		TimerStop:   NewTimerStopHandler(),
		TimerReset:  NewTimerResetHandler(),
	}
}

// RegisterAll registers all clock handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(clockapi.CmdSleep, s.Sleep)
	register(clockapi.CmdNow, s.Now)
	register(clockapi.CmdAfter, s.After)
	register(clockapi.CmdTickerStart, s.TickerStart)
	register(clockapi.CmdTickerNext, s.TickerNext)
	register(clockapi.CmdTickerStop, s.TickerStop)
	register(clockapi.CmdTimerStart, s.TimerStart)
	register(clockapi.CmdTimerWait, s.TimerWait)
	register(clockapi.CmdTimerStop, s.TimerStop)
	register(clockapi.CmdTimerReset, s.TimerReset)
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

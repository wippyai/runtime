// Package clock provides time-related command types for the dispatcher system.
// Commands are pure data structures yielded by processes and handled by clock handlers.
package clock

import (
	"errors"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
)

// Errors returned by clock handlers.
var (
	ErrTimerNotFound  = errors.New("timer not found")
	ErrTickerNotFound = errors.New("ticker not found")
	ErrTickerClosed   = errors.New("ticker closed")
)

// Command IDs for clock operations.
// Range 10-29 is reserved for time commands.
const (
	CmdSleep dispatcher.CommandID = 10 // Pause execution for duration (one-shot)
	CmdNow   dispatcher.CommandID = 13 // Get current time (for deterministic time in workflows)

	// Decomposed ticker pattern (one-shot commands)
	CmdTickerStart dispatcher.CommandID = 14 // Create ticker, returns ID
	CmdTickerNext  dispatcher.CommandID = 15 // Wait for next tick, returns time
	CmdTickerStop  dispatcher.CommandID = 16 // Stop and cleanup ticker

	CmdAfter dispatcher.CommandID = 17 // Returns channel that fires after duration

	// Decomposed timer pattern (one-shot commands)
	CmdTimerStart dispatcher.CommandID = 18 // Create timer, returns ID
	CmdTimerWait  dispatcher.CommandID = 19 // Wait for timer to fire, returns time
	CmdTimerStop  dispatcher.CommandID = 20 // Stop and cleanup timer
	CmdTimerReset dispatcher.CommandID = 21 // Reset timer with new duration
)

func init() {
	dispatcher.MustRegisterCommands("clock",
		CmdSleep, CmdNow,
		CmdTickerStart, CmdTickerNext, CmdTickerStop,
		CmdAfter,
		CmdTimerStart, CmdTimerWait, CmdTimerStop, CmdTimerReset,
	)
}

// SleepCmd requests the scheduler to pause execution for a duration.
// The process yields and resumes after the duration elapses.
// Zero or negative duration completes immediately.
//
// This is a one-shot command - handler returns after duration, no emit calls.
type SleepCmd struct {
	Duration time.Duration
}

// CmdID implements dispatcher.Command.
func (c SleepCmd) CmdID() dispatcher.CommandID {
	return CmdSleep
}

// TickerStartCmd creates a new ticker with given interval.
// Returns ticker ID (uint64) via emit. One-shot command.
type TickerStartCmd struct {
	Duration time.Duration
}

// CmdID implements dispatcher.Command.
func (c TickerStartCmd) CmdID() dispatcher.CommandID {
	return CmdTickerStart
}

// TickerNextCmd waits for the next tick from an existing ticker.
// Returns tick time (int64 nanoseconds) via emit. Blocks until tick available.
// One-shot command - call repeatedly to receive multiple ticks.
type TickerNextCmd struct {
	TickerID uint64
}

// CmdID implements dispatcher.Command.
func (c TickerNextCmd) CmdID() dispatcher.CommandID {
	return CmdTickerNext
}

// TickerStopCmd stops and cleans up a ticker.
// One-shot command, no emit.
type TickerStopCmd struct {
	TickerID uint64
}

// CmdID implements dispatcher.Command.
func (c TickerStopCmd) CmdID() dispatcher.CommandID {
	return CmdTickerStop
}

// NowCmd requests current time from the clock service.
// Returns time.Time via emit(). For deterministic workflows (Temporal),
// handler returns workflow time instead of real time.
type NowCmd struct{}

// CmdID implements dispatcher.Command.
func (c NowCmd) CmdID() dispatcher.CommandID {
	return CmdNow
}

// AfterCmd creates a channel that receives a value after duration.
// Returns a channel immediately, the channel fires after duration.
// Used for non-blocking timers with select.
type AfterCmd struct {
	Duration time.Duration
}

// CmdID implements dispatcher.Command.
func (c AfterCmd) CmdID() dispatcher.CommandID {
	return CmdAfter
}

// TimerStartCmd creates a new one-shot timer with given duration.
// Returns timer ID (uint64) via emit. One-shot command.
type TimerStartCmd struct {
	Duration time.Duration
}

// CmdID implements dispatcher.Command.
func (c TimerStartCmd) CmdID() dispatcher.CommandID {
	return CmdTimerStart
}

// TimerWaitCmd waits for a timer to fire.
// Returns fire time (int64 nanoseconds) via emit. Blocks until timer fires.
// One-shot command - timer is consumed after this call.
type TimerWaitCmd struct {
	TimerID uint64
}

// CmdID implements dispatcher.Command.
func (c TimerWaitCmd) CmdID() dispatcher.CommandID {
	return CmdTimerWait
}

// TimerStopCmd stops and cleans up a timer before it fires.
// One-shot command, returns bool (true if stopped before firing).
type TimerStopCmd struct {
	TimerID uint64
}

// CmdID implements dispatcher.Command.
func (c TimerStopCmd) CmdID() dispatcher.CommandID {
	return CmdTimerStop
}

// TimerResetCmd resets a timer with a new duration.
// Returns bool (true if timer was active and reset, false if already fired).
type TimerResetCmd struct {
	TimerID  uint64
	Duration time.Duration
}

// CmdID implements dispatcher.Command.
func (c TimerResetCmd) CmdID() dispatcher.CommandID {
	return CmdTimerReset
}

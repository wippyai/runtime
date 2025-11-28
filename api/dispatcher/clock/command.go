// Package clock provides time-related command types for the dispatcher system.
// Commands are pure data structures yielded by processes and handled by clock handlers.
package clock

import (
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
)

// Command IDs for clock operations.
// Range 10-49 is reserved for time commands.
const (
	CmdSleep  dispatcher.CommandID = 10 // Pause execution for duration (one-shot)
	CmdTicker dispatcher.CommandID = 11 // Legacy streaming ticker (deprecated)
	CmdTimer  dispatcher.CommandID = 12 // One-shot timer, emits time when fired
	CmdNow    dispatcher.CommandID = 13 // Get current time (for deterministic time in workflows)

	// Streaming ticker decomposed into one-shot commands
	CmdTickerStart dispatcher.CommandID = 14 // Create ticker, returns ID
	CmdTickerNext  dispatcher.CommandID = 15 // Wait for next tick, returns time
	CmdTickerStop  dispatcher.CommandID = 16 // Stop and cleanup ticker
)

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

// TickerCmd creates a ticker that emits at regular intervals.
// Deprecated: Use TickerStartCmd/TickerNextCmd/TickerStopCmd for cleaner streaming.
type TickerCmd struct {
	Duration time.Duration
}

// CmdID implements dispatcher.Command.
func (c TickerCmd) CmdID() dispatcher.CommandID {
	return CmdTicker
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

// TimerCmd creates a one-shot timer that fires after duration.
// Unlike sleep, emits the fire time via emit() before returning.
type TimerCmd struct {
	Duration time.Duration
}

// CmdID implements dispatcher.Command.
func (c TimerCmd) CmdID() dispatcher.CommandID {
	return CmdTimer
}

// NowCmd requests current time from the clock service.
// Returns time.Time via emit(). For deterministic workflows (Temporal),
// handler returns workflow time instead of real time.
type NowCmd struct{}

// CmdID implements dispatcher.Command.
func (c NowCmd) CmdID() dispatcher.CommandID {
	return CmdNow
}

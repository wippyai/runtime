// Package clock provides time-related command types for the dispatcher system.
// Commands are pure data structures yielded by processes and handled by clock handlers.
package clock

import (
	"errors"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
)

// Command IDs for clock operations.
// Range 10-29 is reserved for time commands.
const (
	Sleep dispatcher.CommandID = 10 // Pause execution for duration (one-shot)

	// Decomposed ticker pattern (one-shot commands)
	TickerStart dispatcher.CommandID = 14 // Create ticker, returns ID
	TickerNext  dispatcher.CommandID = 15 // Wait for next tick, returns time
	TickerStop  dispatcher.CommandID = 16 // Stop and cleanup ticker

	// Decomposed timer pattern (one-shot commands)
	TimerStart dispatcher.CommandID = 18 // Create timer, returns ID
	TimerWait  dispatcher.CommandID = 19 // Wait for timer to fire, returns time
	TimerStop  dispatcher.CommandID = 20 // Stop and cleanup timer
	TimerReset dispatcher.CommandID = 21 // Reset timer with new duration
)

// Errors returned by clock handlers.
var (
	ErrTimerNotFound  = errors.New("timer not found")
	ErrTickerNotFound = errors.New("ticker not found")
	ErrTickerClosed   = errors.New("ticker closed")
)

func init() {
	dispatcher.MustRegisterCommands("clock",
		Sleep,
		TickerStart, TickerNext, TickerStop,
		TimerStart, TimerWait, TimerStop, TimerReset,
	)
}

type (
	// SleepCmd requests the scheduler to pause execution for a duration.
	// The process yields and resumes after the duration elapses.
	// Zero or negative duration completes immediately.
	//
	// This is a one-shot command - handler returns after duration, no emit calls.
	SleepCmd struct {
		Duration time.Duration
	}

	// TickerStartCmd creates a new ticker with given interval.
	// Returns ticker ID (uint64) via emit. One-shot command.
	TickerStartCmd struct {
		Duration time.Duration
	}

	// TickerNextCmd waits for the next tick from an existing ticker.
	// Returns tick time (int64 nanoseconds) via emit. Blocks until tick available.
	// One-shot command - call repeatedly to receive multiple ticks.
	TickerNextCmd struct {
		TickerID uint64
	}

	// TickerStopCmd stops and cleans up a ticker.
	// One-shot command, no emit.
	TickerStopCmd struct {
		TickerID uint64
	}

	// TimerStartCmd creates a new one-shot timer with given duration.
	// Returns timer ID (uint64) via emit. One-shot command.
	TimerStartCmd struct {
		Duration time.Duration
	}

	// TimerWaitCmd waits for a timer to fire.
	// Returns fire time (int64 nanoseconds) via emit. Blocks until timer fires.
	// One-shot command - timer is consumed after this call.
	TimerWaitCmd struct {
		TimerID uint64
	}

	// TimerStopCmd stops and cleans up a timer before it fires.
	// One-shot command, returns bool (true if stopped before firing).
	TimerStopCmd struct {
		TimerID uint64
	}

	// TimerResetCmd resets a timer with a new duration.
	// Returns bool (true if timer was active and reset, false if already fired).
	TimerResetCmd struct {
		TimerID  uint64
		Duration time.Duration
	}
)

// CmdID implements dispatcher.Command.
func (c SleepCmd) CmdID() dispatcher.CommandID { return Sleep }

// CmdID implements dispatcher.Command.
func (c TickerStartCmd) CmdID() dispatcher.CommandID { return TickerStart }

// CmdID implements dispatcher.Command.
func (c TickerNextCmd) CmdID() dispatcher.CommandID { return TickerNext }

// CmdID implements dispatcher.Command.
func (c TickerStopCmd) CmdID() dispatcher.CommandID { return TickerStop }

// CmdID implements dispatcher.Command.
func (c TimerStartCmd) CmdID() dispatcher.CommandID { return TimerStart }

// CmdID implements dispatcher.Command.
func (c TimerWaitCmd) CmdID() dispatcher.CommandID { return TimerWait }

// CmdID implements dispatcher.Command.
func (c TimerStopCmd) CmdID() dispatcher.CommandID { return TimerStop }

// CmdID implements dispatcher.Command.
func (c TimerResetCmd) CmdID() dispatcher.CommandID { return TimerReset }

// TickerStartResult is returned by TickerStart command with cleanup callback.
type TickerStartResult struct {
	ID   uint64
	Stop func() // Cleanup function to stop the ticker
}

// TimerStartResult is returned by TimerStart command with cleanup callback.
type TimerStartResult struct {
	ID   uint64
	Stop func() // Cleanup function to stop the timer
}

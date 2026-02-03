// Package clock provides time-related command types for the dispatcher system.
// Commands are pure data structures yielded by processes and handled by clock handlers.
package clock

import (
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
)

// Command IDs for clock operations.
// Range 10-29 is reserved for time commands.
const (
	Sleep dispatcher.CommandID = 10 // Pause execution for duration (one-shot)

	// TickerStart creates a ticker that uses topic-based delivery like events/websocket
	TickerStart dispatcher.CommandID = 14 // Create ticker, sends ticks to topic
	TickerStop  dispatcher.CommandID = 16 // Stop and cleanup ticker

	// TimerStart creates a timer using decomposed timer pattern (one-shot command)
	TimerStart dispatcher.CommandID = 18 // Create timer, returns ID
	TimerWait  dispatcher.CommandID = 19 // Wait for timer to fire, returns time
	TimerStop  dispatcher.CommandID = 20 // Stop and cleanup timer
	TimerReset dispatcher.CommandID = 21 // Reset timer with new duration
)

func init() {
	dispatcher.MustRegisterCommands("clock",
		Sleep,
		TickerStart, TickerStop,
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

	// TickerStartCmd creates a new ticker that sends ticks to a topic.
	// The dispatcher sends tick times to the process via relay topic.
	// Returns ticker ID (uint64) via emit. Ticker runs until stopped.
	TickerStartCmd struct {
		PID      pid.PID
		Topic    string
		Duration time.Duration
	}

	// TickerStopCmd stops and cleans up a ticker.
	TickerStopCmd struct {
		TickerID uint64
	}

	// TimerStartCmd creates a new one-shot timer with given duration.
	// Uses topic-based delivery like ticker - sends fire time to topic when complete.
	// Returns timer ID (uint64) via emit.
	TimerStartCmd struct {
		PID      pid.PID
		Topic    string
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
	Stop func()
	ID   uint64
}

// TimerStartResult is returned by TimerStart command with cleanup callback.
type TimerStartResult struct {
	Stop func()
	ID   uint64
}

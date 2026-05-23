// SPDX-License-Identifier: MPL-2.0

// Package clock provides time-related command types for the dispatcher system.
// Commands are pure data structures yielded by processes and handled by clock handlers.
package clock

import (
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
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

	// Stop-by-chID variants used by the engine ephemeral channel router to
	// cancel timers/tickers without holding the dispatcher-internal ID.
	// The dispatcher maintains a reverse map (pid, epoch, chID) → internalID.
	// A stop arriving before the corresponding start is tombstoned and
	// consumed by the late start so no orphan timer can leak.
	TimerStopByChID  dispatcher.CommandID = 22
	TickerStopByChID dispatcher.CommandID = 23
)

func init() {
	dispatcher.MustRegisterCommands("clock",
		Sleep,
		TickerStart, TickerStop,
		TimerStart, TimerWait, TimerStop, TimerReset,
		TimerStopByChID, TickerStopByChID,
	)
}

// FireBuilder produces the payload to deliver when a timer or ticker
// fires. Used by the engine ephemeral channel router so the clock
// dispatcher does not need to know the EphemeralFrame envelope format.
//
// genRef (when supplied) is the live atomic generation counter for the
// ephemeral router entry; the builder reads it via .Load() at fire time
// so stale frames carry an outdated gen and are dropped on the process
// side after a timer reset.
//
// When nil, the dispatcher falls back to its legacy int64-nanos payload
// for backward compatibility with non-router callers (workflow timers).
type FireBuilder func(at time.Time, gen uint64) payload.Payload

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
	//
	// ChID + Epoch identify the ephemeral router entry that owns this
	// ticker; the dispatcher uses them as a reverse-map key so the entry
	// can be cancelled via TickerStopByChID. GenRef is the live atomic
	// gen counter the engine router uses to detect stale fires after a
	// reset; the fire callback reads it via .Load() and stamps every
	// frame with the current value. Build is the FireBuilder that turns
	// (at, gen) into the on-wire payload. All four are zero/nil for
	// non-router callers (workflow timers), where the dispatcher falls
	// back to its legacy int64-nanos payload.
	TickerStartCmd struct {
		GenRef   *atomic.Uint64
		Build    FireBuilder
		Topic    string
		PID      pid.PID
		Duration time.Duration
		ChID     uint64
		Epoch    uint64
	}

	// TickerStopCmd stops and cleans up a ticker.
	TickerStopCmd struct {
		TickerID uint64
	}

	// TimerStartCmd creates a new one-shot timer with given duration.
	// Uses topic-based delivery like ticker - sends fire time to topic when complete.
	// Returns timer ID (uint64) via emit.
	//
	// See TickerStartCmd for the ChID / Epoch / GenRef / Build fields used
	// by the engine ephemeral channel router.
	TimerStartCmd struct {
		GenRef   *atomic.Uint64
		Build    FireBuilder
		Topic    string
		PID      pid.PID
		Duration time.Duration
		ChID     uint64
		Epoch    uint64
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

	// TimerStopByChIDCmd cancels a router-tagged timer by the (Epoch, ChID)
	// pair the start command was issued with. Used by the engine to drain
	// timers when a process exits before the matching TimerStartCmd has
	// returned its internal ID. If the dispatcher has not yet processed
	// the start, this command leaves a tombstone that the late start
	// consumes (it completes its yield with ErrStoppedBeforeStart and
	// does NOT schedule the Go timer).
	TimerStopByChIDCmd struct {
		TargetPID pid.PID
		Epoch     uint64
		ChID      uint64
	}

	// TickerStopByChIDCmd is the ticker analog of TimerStopByChIDCmd.
	TickerStopByChIDCmd struct {
		TargetPID pid.PID
		Epoch     uint64
		ChID      uint64
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

// CmdID implements dispatcher.Command.
func (c TimerStopByChIDCmd) CmdID() dispatcher.CommandID { return TimerStopByChID }

// CmdID implements dispatcher.Command.
func (c TickerStopByChIDCmd) CmdID() dispatcher.CommandID { return TickerStopByChID }

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

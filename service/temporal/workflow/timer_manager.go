// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"sync/atomic"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	"github.com/wippyai/runtime/service/temporal/propagator"
	commonpb "go.temporal.io/api/common/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

// timerEntry tracks an active timer.
type timerEntry struct {
	// build is set when the command targets the engine ephemeral router
	// (cmd.Build != nil at start). The fire callback uses it to wrap
	// the tick value in an EphemeralFrame so the routed process can
	// decode the payload identically to a clock-dispatched fire.
	build  clockapi.FireBuilder
	genRef *atomic.Uint64
	Topic  string
	PID    pid.PID
	// epoch + chID are non-zero when the timer was started by an
	// engine ephemeral router yield. They let StopByChID find the
	// entry without holding the internal id.
	epoch    uint64
	chID     uint64
	ID       uint64
	Duration time.Duration
	Canceled bool
}

// tickerEntry tracks an active ticker.
type tickerEntry struct {
	build    clockapi.FireBuilder
	genRef   *atomic.Uint64
	Topic    string
	PID      pid.PID
	epoch    uint64
	chID     uint64
	ID       uint64
	Duration time.Duration
	Stopped  bool
}

// buildFirePayload constructs the on-wire payload for a timer/ticker fire.
// When the start command set a FireBuilder, it is used (router path);
// otherwise we fall back to the legacy int64-nanos payload.
func buildFirePayload(b clockapi.FireBuilder, genRef *atomic.Uint64, at time.Time) payload.Payload {
	if b == nil {
		return payload.NewPayload(at.UnixNano(), payload.Golang)
	}
	var gen uint64
	if genRef != nil {
		gen = genRef.Load()
	}
	if p := b(at, gen); p != nil {
		return p
	}
	return payload.NewPayload(at.UnixNano(), payload.Golang)
}

// TimerManager manages timers and tickers in workflow context.
type TimerManager struct {
	env           bindings.WorkflowEnvironment
	replayLog     *propagator.ReplayLogger
	timers        map[uint64]*timerEntry
	tickers       map[uint64]*tickerEntry
	signalQueue   *[]incomingSignal
	timerCounter  uint64
	tickerCounter uint64
}

// NewTimerManager creates a new timer manager.
func NewTimerManager(env bindings.WorkflowEnvironment, replayLog *propagator.ReplayLogger, signalQueue *[]incomingSignal) *TimerManager {
	return &TimerManager{
		env:         env,
		replayLog:   replayLog,
		timers:      make(map[uint64]*timerEntry),
		tickers:     make(map[uint64]*tickerEntry),
		signalQueue: signalQueue,
	}
}

// Sleep handles time.sleep command - blocks for a duration.
func (m *TimerManager) Sleep(duration time.Duration, resume func(data any, err error)) {
	m.env.NewTimer(duration, workflow.TimerOptions{}, func(_ *commonpb.Payloads, err error) {
		if err != nil {
			resume(nil, temporalerrors.FromTemporalError(err))
			return
		}
		resume(payload.NewPayload(true, payload.Golang), nil)
	})
}

// StartTimer handles time.after/time.timer - creates a one-shot timer.
func (m *TimerManager) StartTimer(cmd clockapi.TimerStartCmd, resume func(data any, err error)) {
	if cmd.Duration <= 0 {
		resume(clockapi.TimerStartResult{ID: 0}, nil)
		return
	}

	m.timerCounter++
	timerID := m.timerCounter

	timer := &timerEntry{
		ID:       timerID,
		PID:      cmd.PID,
		Topic:    cmd.Topic,
		Duration: cmd.Duration,
		build:    cmd.Build,
		genRef:   cmd.GenRef,
		epoch:    cmd.Epoch,
		chID:     cmd.ChID,
	}
	m.timers[timerID] = timer

	m.env.NewTimer(cmd.Duration, workflow.TimerOptions{}, func(_ *commonpb.Payloads, err error) {
		t, ok := m.timers[timerID]
		if !ok || t.Canceled {
			return
		}
		delete(m.timers, timerID)

		if err != nil {
			m.replayLog.Debug("timer error", zap.Uint64("timer_id", timerID), zap.Error(err))
			return
		}

		*m.signalQueue = append(*m.signalQueue, incomingSignal{
			Name:     t.Topic,
			Payloads: payload.Payloads{buildFirePayload(t.build, t.genRef, m.env.Now())},
		})
	})

	resume(clockapi.TimerStartResult{
		ID: timerID,
		Stop: func() {
			if t, ok := m.timers[timerID]; ok {
				t.Canceled = true
				delete(m.timers, timerID)
			}
		},
	}, nil)
}

// StopTimer cancels an active timer.
func (m *TimerManager) StopTimer(timerID uint64, resume func(data any, err error)) {
	timer, ok := m.timers[timerID]
	if !ok {
		resume(false, nil)
		return
	}

	timer.Canceled = true
	delete(m.timers, timerID)
	resume(true, nil)
}

// ResetTimer resets a timer with a new duration.
func (m *TimerManager) ResetTimer(timerID uint64, duration time.Duration, resume func(data any, err error)) {
	timer, ok := m.timers[timerID]
	if !ok {
		resume(false, nil)
		return
	}

	timer.Canceled = true
	delete(m.timers, timerID)

	newTimer := &timerEntry{
		ID:       timerID,
		PID:      timer.PID,
		Topic:    timer.Topic,
		Duration: duration,
		build:    timer.build,
		genRef:   timer.genRef,
	}
	m.timers[timerID] = newTimer

	m.env.NewTimer(duration, workflow.TimerOptions{}, func(_ *commonpb.Payloads, err error) {
		t, ok := m.timers[timerID]
		if !ok || t.Canceled {
			return
		}
		delete(m.timers, timerID)

		if err != nil {
			m.replayLog.Debug("timer reset error", zap.Uint64("timer_id", timerID), zap.Error(err))
			return
		}

		*m.signalQueue = append(*m.signalQueue, incomingSignal{
			Name:     t.Topic,
			Payloads: payload.Payloads{buildFirePayload(t.build, t.genRef, m.env.Now())},
		})
	})

	resume(true, nil)
}

// StartTicker creates a repeating ticker.
func (m *TimerManager) StartTicker(cmd clockapi.TickerStartCmd, resume func(data any, err error)) {
	if cmd.Duration <= 0 {
		resume(clockapi.TickerStartResult{ID: 0}, nil)
		return
	}

	m.tickerCounter++
	tickerID := m.tickerCounter

	ticker := &tickerEntry{
		ID:       tickerID,
		PID:      cmd.PID,
		Topic:    cmd.Topic,
		Duration: cmd.Duration,
		build:    cmd.Build,
		genRef:   cmd.GenRef,
		epoch:    cmd.Epoch,
		chID:     cmd.ChID,
	}
	m.tickers[tickerID] = ticker

	m.scheduleNextTick(tickerID)

	resume(clockapi.TickerStartResult{
		ID: tickerID,
		Stop: func() {
			if t, ok := m.tickers[tickerID]; ok {
				t.Stopped = true
				delete(m.tickers, tickerID)
			}
		},
	}, nil)
}

// StopTicker stops a running ticker.
func (m *TimerManager) StopTicker(tickerID uint64, resume func(data any, err error)) {
	ticker, ok := m.tickers[tickerID]
	if !ok {
		resume(nil, nil)
		return
	}

	ticker.Stopped = true
	delete(m.tickers, tickerID)
	resume(nil, nil)
}

// StopTimerByChID cancels a timer identified by its router (epoch, chID)
// rather than the internal id. Used by the engine ephemeral router
// migration of time.timer:stop().
func (m *TimerManager) StopTimerByChID(epoch, chID uint64, resume func(data any, err error)) {
	for id, t := range m.timers {
		if t.epoch == epoch && t.chID == chID {
			t.Canceled = true
			delete(m.timers, id)
			resume(true, nil)
			return
		}
	}
	resume(false, nil)
}

// StopTickerByChID cancels a ticker identified by its router (epoch, chID).
func (m *TimerManager) StopTickerByChID(epoch, chID uint64, resume func(data any, err error)) {
	for id, t := range m.tickers {
		if t.epoch == epoch && t.chID == chID {
			t.Stopped = true
			delete(m.tickers, id)
			resume(true, nil)
			return
		}
	}
	resume(false, nil)
}

// scheduleNextTick schedules the next tick for a ticker.
func (m *TimerManager) scheduleNextTick(tickerID uint64) {
	ticker, ok := m.tickers[tickerID]
	if !ok || ticker.Stopped {
		return
	}

	m.env.NewTimer(ticker.Duration, workflow.TimerOptions{}, func(_ *commonpb.Payloads, err error) {
		t, ok := m.tickers[tickerID]
		if !ok || t.Stopped {
			return
		}

		if err != nil {
			m.replayLog.Debug("ticker error", zap.Uint64("ticker_id", tickerID), zap.Error(err))
			return
		}

		*m.signalQueue = append(*m.signalQueue, incomingSignal{
			Name:     t.Topic,
			Payloads: payload.Payloads{buildFirePayload(t.build, t.genRef, m.env.Now())},
		})

		m.scheduleNextTick(tickerID)
	})
}

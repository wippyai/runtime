// SPDX-License-Identifier: MPL-2.0

package workflow

import (
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
	PID      pid.PID
	Topic    string
	ID       uint64
	Duration time.Duration
	Canceled bool
}

// tickerEntry tracks an active ticker.
type tickerEntry struct {
	PID      pid.PID
	Topic    string
	ID       uint64
	Duration time.Duration
	Stopped  bool
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

		fireTime := m.env.Now().UnixNano()
		*m.signalQueue = append(*m.signalQueue, incomingSignal{
			Name:     t.Topic,
			Payloads: payload.Payloads{payload.NewPayload(fireTime, payload.Golang)},
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

		fireTime := m.env.Now().UnixNano()
		*m.signalQueue = append(*m.signalQueue, incomingSignal{
			Name:     t.Topic,
			Payloads: payload.Payloads{payload.NewPayload(fireTime, payload.Golang)},
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

		tickTime := m.env.Now().UnixNano()
		*m.signalQueue = append(*m.signalQueue, incomingSignal{
			Name:     t.Topic,
			Payloads: payload.Payloads{payload.NewPayload(tickTime, payload.Golang)},
		})

		m.scheduleNextTick(tickerID)
	})
}

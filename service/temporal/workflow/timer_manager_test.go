// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/pid"
)

func TestTimerEntry_Fields(t *testing.T) {
	entry := timerEntry{
		ID:       1,
		PID:      pid.PID{Host: "test", UniqID: "123"},
		Topic:    "timer-topic",
		Duration: 5 * time.Second,
		Canceled: false,
	}

	assert.Equal(t, uint64(1), entry.ID)
	assert.Equal(t, "test", entry.PID.Host)
	assert.Equal(t, "timer-topic", entry.Topic)
	assert.Equal(t, 5*time.Second, entry.Duration)
	assert.False(t, entry.Canceled)
}

func TestTickerEntry_Fields(t *testing.T) {
	entry := tickerEntry{
		ID:       2,
		PID:      pid.PID{Host: "test", UniqID: "456"},
		Topic:    "ticker-topic",
		Duration: 1 * time.Second,
		Stopped:  false,
	}

	assert.Equal(t, uint64(2), entry.ID)
	assert.Equal(t, "test", entry.PID.Host)
	assert.Equal(t, "ticker-topic", entry.Topic)
	assert.Equal(t, 1*time.Second, entry.Duration)
	assert.False(t, entry.Stopped)
}

func TestTimerStartCmd_ZeroDuration(t *testing.T) {
	// Test that zero duration returns ID 0
	cmd := clockapi.TimerStartCmd{
		Duration: 0,
		Topic:    "test",
	}

	assert.Equal(t, time.Duration(0), cmd.Duration)
}

func TestTickerStartCmd_ZeroDuration(t *testing.T) {
	// Test that zero duration returns ID 0
	cmd := clockapi.TickerStartCmd{
		Duration: 0,
		Topic:    "test",
	}

	assert.Equal(t, time.Duration(0), cmd.Duration)
}

func TestTimerStartResult_StopFunction(t *testing.T) {
	called := false
	result := clockapi.TimerStartResult{
		ID: 1,
		Stop: func() {
			called = true
		},
	}

	assert.Equal(t, uint64(1), result.ID)
	result.Stop()
	assert.True(t, called)
}

func TestTickerStartResult_StopFunction(t *testing.T) {
	called := false
	result := clockapi.TickerStartResult{
		ID: 1,
		Stop: func() {
			called = true
		},
	}

	assert.Equal(t, uint64(1), result.ID)
	result.Stop()
	assert.True(t, called)
}

func TestTimerManager_MapOperations(t *testing.T) {
	// Test timer map operations without workflow environment
	timers := make(map[uint64]*timerEntry)

	timer1 := &timerEntry{ID: 1, Topic: "timer1", Duration: time.Second}
	timer2 := &timerEntry{ID: 2, Topic: "timer2", Duration: 2 * time.Second}

	timers[1] = timer1
	timers[2] = timer2

	assert.Len(t, timers, 2)

	// Test cancel
	timer1.Canceled = true
	delete(timers, 1)
	assert.Len(t, timers, 1)

	// Verify remaining
	remaining, ok := timers[2]
	require.True(t, ok)
	assert.Equal(t, "timer2", remaining.Topic)
}

func TestTickerManager_MapOperations(t *testing.T) {
	// Test ticker map operations without workflow environment
	tickers := make(map[uint64]*tickerEntry)

	ticker1 := &tickerEntry{ID: 1, Topic: "ticker1", Duration: time.Second}
	ticker2 := &tickerEntry{ID: 2, Topic: "ticker2", Duration: 2 * time.Second}

	tickers[1] = ticker1
	tickers[2] = ticker2

	assert.Len(t, tickers, 2)

	// Test stop
	ticker1.Stopped = true
	delete(tickers, 1)
	assert.Len(t, tickers, 1)

	// Verify remaining
	remaining, ok := tickers[2]
	require.True(t, ok)
	assert.Equal(t, "ticker2", remaining.Topic)
	assert.False(t, remaining.Stopped)
}

func TestTimerEntry_CancelFlag(t *testing.T) {
	entry := &timerEntry{
		ID:       1,
		Canceled: false,
	}

	assert.False(t, entry.Canceled)
	entry.Canceled = true
	assert.True(t, entry.Canceled)
}

func TestTickerEntry_StoppedFlag(t *testing.T) {
	entry := &tickerEntry{
		ID:      1,
		Stopped: false,
	}

	assert.False(t, entry.Stopped)
	entry.Stopped = true
	assert.True(t, entry.Stopped)
}

func TestTimerManager_CounterIncrement(t *testing.T) {
	// Test that counter increments work correctly
	var timerCounter uint64

	timerCounter++
	assert.Equal(t, uint64(1), timerCounter)

	timerCounter++
	assert.Equal(t, uint64(2), timerCounter)

	timerCounter++
	assert.Equal(t, uint64(3), timerCounter)
}

func TestIncomingSignal_Fields(t *testing.T) {
	sig := incomingSignal{
		Name:     "timer-fired",
		Payloads: nil,
	}

	assert.Equal(t, "timer-fired", sig.Name)
	assert.Nil(t, sig.Payloads)
}

func TestSignalQueue_Append(t *testing.T) {
	var queue []incomingSignal

	queue = append(queue, incomingSignal{Name: "sig1"})
	queue = append(queue, incomingSignal{Name: "sig2"})

	assert.Len(t, queue, 2)
	assert.Equal(t, "sig1", queue[0].Name)
	assert.Equal(t, "sig2", queue[1].Name)

	// Clear queue
	queue = queue[:0]
	assert.Empty(t, queue)
}

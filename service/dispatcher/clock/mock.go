package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	clockapi "github.com/wippyai/runtime/api/dispatcher/clock"
)

// MockClock provides controllable time for testing.
// Allows advancing time manually to trigger sleeps/timers instantly.
type MockClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewMockClock creates a mock clock starting at the given time.
func NewMockClock(start time.Time) *MockClock {
	return &MockClock{now: start}
}

// Now returns current mock time.
func (c *MockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves mock time forward by duration.
func (c *MockClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// Set sets mock time to specific value.
func (c *MockClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

// MockSleepHandler processes sleep commands instantly for testing.
// Tracks sleep durations and advances mock clock.
type MockSleepHandler struct {
	clock  *MockClock
	mu     sync.Mutex
	sleeps []clockapi.SleepCmd
	count  atomic.Int64
}

// NewMockSleepHandler creates a mock sleep handler with optional clock.
// If clock is nil, sleeps complete instantly without time tracking.
func NewMockSleepHandler(clock *MockClock) *MockSleepHandler {
	return &MockSleepHandler{clock: clock}
}

// Handle implements dispatcher.Handler.
// Completes immediately, advances mock clock, records duration.
func (h *MockSleepHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	sleep := cmd.(clockapi.SleepCmd)
	h.mu.Lock()
	h.sleeps = append(h.sleeps, sleep)
	h.mu.Unlock()
	h.count.Add(1)

	if h.clock != nil && sleep.Duration > 0 {
		h.clock.Advance(sleep.Duration)
	}
	return nil
}

// Sleeps returns recorded sleep commands.
func (h *MockSleepHandler) Sleeps() []clockapi.SleepCmd {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]clockapi.SleepCmd, len(h.sleeps))
	copy(result, h.sleeps)
	return result
}

// Count returns total number of handled sleeps.
func (h *MockSleepHandler) Count() int64 {
	return h.count.Load()
}

// Reset clears recorded sleeps.
func (h *MockSleepHandler) Reset() {
	h.mu.Lock()
	h.sleeps = h.sleeps[:0]
	h.mu.Unlock()
	h.count.Store(0)
}

// MockTimerHandler handles timer commands for testing.
// Emits mock time and advances clock.
// MockTimerStartHandler creates mock timers for testing.
type MockTimerStartHandler struct {
	clock  *MockClock
	count  atomic.Int64
	nextID atomic.Uint64
	timers sync.Map // map[uint64]time.Duration
}

// NewMockTimerStartHandler creates a mock timer start handler.
func NewMockTimerStartHandler(clock *MockClock) *MockTimerStartHandler {
	return &MockTimerStartHandler{clock: clock}
}

// Handle implements dispatcher.Handler.
func (h *MockTimerStartHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	timer := cmd.(clockapi.TimerStartCmd)
	h.count.Add(1)

	id := h.nextID.Add(1)
	h.timers.Store(id, timer.Duration)
	emit(id)
	return nil
}

// Count returns total number of timers created.
func (h *MockTimerStartHandler) Count() int64 {
	return h.count.Load()
}

// MockTimerWaitHandler waits for mock timers.
type MockTimerWaitHandler struct {
	clock  *MockClock
	count  atomic.Int64
	timers *sync.Map
}

// NewMockTimerWaitHandler creates a mock timer wait handler.
func NewMockTimerWaitHandler(clock *MockClock, timers *sync.Map) *MockTimerWaitHandler {
	return &MockTimerWaitHandler{clock: clock, timers: timers}
}

// Handle implements dispatcher.Handler.
func (h *MockTimerWaitHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	wait := cmd.(clockapi.TimerWaitCmd)
	h.count.Add(1)

	if d, ok := h.timers.LoadAndDelete(wait.TimerID); ok {
		duration := d.(time.Duration)
		if h.clock != nil && duration > 0 {
			h.clock.Advance(duration)
		}
	}
	emit(h.clock.Now().UnixNano())
	return nil
}

// Count returns total timer waits.
func (h *MockTimerWaitHandler) Count() int64 {
	return h.count.Load()
}

// MockTimerStopHandler stops mock timers.
type MockTimerStopHandler struct {
	count  atomic.Int64
	timers *sync.Map
}

// NewMockTimerStopHandler creates a mock timer stop handler.
func NewMockTimerStopHandler(timers *sync.Map) *MockTimerStopHandler {
	return &MockTimerStopHandler{timers: timers}
}

// Handle implements dispatcher.Handler.
func (h *MockTimerStopHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	stop := cmd.(clockapi.TimerStopCmd)
	h.count.Add(1)

	_, stopped := h.timers.LoadAndDelete(stop.TimerID)
	emit(stopped)
	return nil
}

// Count returns total timer stops.
func (h *MockTimerStopHandler) Count() int64 {
	return h.count.Load()
}

// MockTimerResetHandler resets mock timers.
type MockTimerResetHandler struct {
	clock  *MockClock
	count  atomic.Int64
	timers *sync.Map
}

// NewMockTimerResetHandler creates a mock timer reset handler.
func NewMockTimerResetHandler(clock *MockClock, timers *sync.Map) *MockTimerResetHandler {
	return &MockTimerResetHandler{clock: clock, timers: timers}
}

// Handle implements dispatcher.Handler.
func (h *MockTimerResetHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	reset := cmd.(clockapi.TimerResetCmd)
	h.count.Add(1)

	if _, ok := h.timers.Load(reset.TimerID); ok {
		h.timers.Store(reset.TimerID, reset.Duration)
		emit(true)
	} else {
		emit(false)
	}
	return nil
}

// Count returns total timer resets.
func (h *MockTimerResetHandler) Count() int64 {
	return h.count.Load()
}

// MockNowHandler returns mock time for testing.
type MockNowHandler struct {
	clock *MockClock
	count atomic.Int64
}

// NewMockNowHandler creates a mock now handler.
func NewMockNowHandler(clock *MockClock) *MockNowHandler {
	return &MockNowHandler{clock: clock}
}

// Handle implements dispatcher.Handler.
// Emits mock time as nanoseconds.
func (h *MockNowHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	h.count.Add(1)
	emit(h.clock.Now().UnixNano())
	return nil
}

// Count returns total number of now calls.
func (h *MockNowHandler) Count() int64 {
	return h.count.Load()
}

// MockService bundles all mock handlers for testing.
type MockService struct {
	Clock      *MockClock
	Sleep      *MockSleepHandler
	TimerStart *MockTimerStartHandler
	TimerWait  *MockTimerWaitHandler
	TimerStop  *MockTimerStopHandler
	TimerReset *MockTimerResetHandler
	Now        *MockNowHandler
}

// NewMockService creates a mock clock service.
func NewMockService() *MockService {
	clock := NewMockClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	timerStart := NewMockTimerStartHandler(clock)
	return &MockService{
		Clock:      clock,
		Sleep:      NewMockSleepHandler(clock),
		TimerStart: timerStart,
		TimerWait:  NewMockTimerWaitHandler(clock, &timerStart.timers),
		TimerStop:  NewMockTimerStopHandler(&timerStart.timers),
		TimerReset: NewMockTimerResetHandler(clock, &timerStart.timers),
		Now:        NewMockNowHandler(clock),
	}
}

// RegisterAll registers all mock handlers.
func (s *MockService) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(clockapi.CmdSleep, s.Sleep)
	register(clockapi.CmdTimerStart, s.TimerStart)
	register(clockapi.CmdTimerWait, s.TimerWait)
	register(clockapi.CmdTimerStop, s.TimerStop)
	register(clockapi.CmdTimerReset, s.TimerReset)
	register(clockapi.CmdNow, s.Now)
}

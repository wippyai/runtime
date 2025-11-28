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
type MockTimerHandler struct {
	clock *MockClock
	count atomic.Int64
}

// NewMockTimerHandler creates a mock timer handler.
func NewMockTimerHandler(clock *MockClock) *MockTimerHandler {
	return &MockTimerHandler{clock: clock}
}

// Handle implements dispatcher.Handler.
// Advances clock by duration, emits resulting time.
func (h *MockTimerHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	timer := cmd.(clockapi.TimerCmd)
	h.count.Add(1)

	if h.clock != nil && timer.Duration > 0 {
		h.clock.Advance(timer.Duration)
	}
	emit(h.clock.Now())
	return nil
}

// Count returns total number of handled timers.
func (h *MockTimerHandler) Count() int64 {
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

// MockTickerHandler handles ticker commands for testing.
// Emits a configurable number of ticks immediately.
type MockTickerHandler struct {
	clock     *MockClock
	tickCount int
	emitCount atomic.Int64
}

// NewMockTickerHandler creates a mock ticker handler.
// tickCount specifies how many ticks to emit before returning.
func NewMockTickerHandler(clock *MockClock, tickCount int) *MockTickerHandler {
	return &MockTickerHandler{clock: clock, tickCount: tickCount}
}

// Handle implements dispatcher.Handler.
// Emits tickCount values immediately, advancing clock each tick.
func (h *MockTickerHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	ticker := cmd.(clockapi.TickerCmd)
	for i := 0; i < h.tickCount; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if h.clock != nil {
				h.clock.Advance(ticker.Duration)
			}
			emit(h.clock.Now())
			h.emitCount.Add(1)
		}
	}
	return nil
}

// EmitCount returns total number of emitted ticks.
func (h *MockTickerHandler) EmitCount() int64 {
	return h.emitCount.Load()
}

// MockService bundles all mock handlers for testing.
type MockService struct {
	Clock  *MockClock
	Sleep  *MockSleepHandler
	Timer  *MockTimerHandler
	Ticker *MockTickerHandler
	Now    *MockNowHandler
}

// NewMockService creates a mock clock service.
// tickCount specifies how many ticks the ticker handler emits.
func NewMockService(tickCount int) *MockService {
	clock := NewMockClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	return &MockService{
		Clock:  clock,
		Sleep:  NewMockSleepHandler(clock),
		Timer:  NewMockTimerHandler(clock),
		Ticker: NewMockTickerHandler(clock, tickCount),
		Now:    NewMockNowHandler(clock),
	}
}

// RegisterAll registers all mock handlers.
func (s *MockService) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(clockapi.CmdSleep, s.Sleep)
	register(clockapi.CmdTicker, s.Ticker)
	register(clockapi.CmdTimer, s.Timer)
	register(clockapi.CmdNow, s.Now)
}

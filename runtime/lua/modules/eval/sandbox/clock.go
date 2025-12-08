package sandbox

import (
	"sync"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	lua "github.com/yuin/gopher-lua"
)

// MockClock provides a controllable time reference for sandbox execution.
// Implements clock.TimeReference interface for use with the scheduler.
type MockClock struct {
	mu        sync.RWMutex
	startTime time.Time
	current   time.Time
}

var _ clockapi.TimeReference = (*MockClock)(nil)

// NewMockClock creates a new mock clock. If startNano is 0, uses current time.
func NewMockClock(startNano int64) *MockClock {
	var t time.Time
	if startNano > 0 {
		t = time.Unix(0, startNano)
	} else {
		t = time.Now()
	}
	return &MockClock{
		startTime: t,
		current:   t,
	}
}

// Now returns the current simulated time.
func (c *MockClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// StartTime returns the start time for os.clock() calculations.
func (c *MockClock) StartTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.startTime
}

// Set changes the current time to an absolute value.
func (c *MockClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = t
}

// SetNano sets time from nanoseconds since epoch.
func (c *MockClock) SetNano(nano int64) {
	c.Set(time.Unix(0, nano))
}

// Advance moves time forward by duration.
func (c *MockClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// AdvanceNano moves time forward by nanoseconds.
func (c *MockClock) AdvanceNano(nano int64) {
	c.Advance(time.Duration(nano))
}

// Elapsed returns duration since start.
func (c *MockClock) Elapsed() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current.Sub(c.startTime)
}

// NowNano returns current time as nanoseconds.
func (c *MockClock) NowNano() int64 {
	return c.Now().UnixNano()
}

// Lua methods for MockClock

var clockMethods = map[string]lua.LGoFunc{
	"now":          clockNow,
	"now_nano":     clockNowNano,
	"set":          clockSet,
	"advance":      clockAdvance,
	"advance_nano": clockAdvanceNano,
	"elapsed":      clockElapsed,
	"start_time":   clockStartTime,
}

func checkClock(l *lua.LState) *MockClock {
	ud := l.CheckUserData(1)
	if c, ok := ud.Value.(*MockClock); ok {
		return c
	}
	l.ArgError(1, "eval.sandbox.Clock expected")
	return nil
}

func clockNow(l *lua.LState) int {
	c := checkClock(l)
	if c == nil {
		return 0
	}
	l.Push(lua.LNumber(c.NowNano()))
	return 1
}

func clockNowNano(l *lua.LState) int {
	c := checkClock(l)
	if c == nil {
		return 0
	}
	l.Push(lua.LNumber(c.NowNano()))
	return 1
}

func clockSet(l *lua.LState) int {
	c := checkClock(l)
	if c == nil {
		return 0
	}
	nano := int64(l.CheckNumber(2))
	c.SetNano(nano)
	return 0
}

func clockAdvance(l *lua.LState) int {
	c := checkClock(l)
	if c == nil {
		return 0
	}
	nano := int64(l.CheckNumber(2))
	c.AdvanceNano(nano)
	return 0
}

func clockAdvanceNano(l *lua.LState) int {
	c := checkClock(l)
	if c == nil {
		return 0
	}
	nano := int64(l.CheckNumber(2))
	c.AdvanceNano(nano)
	return 0
}

func clockElapsed(l *lua.LState) int {
	c := checkClock(l)
	if c == nil {
		return 0
	}
	l.Push(lua.LNumber(c.Elapsed().Nanoseconds()))
	return 1
}

func clockStartTime(l *lua.LState) int {
	c := checkClock(l)
	if c == nil {
		return 0
	}
	l.Push(lua.LNumber(c.StartTime().UnixNano()))
	return 1
}

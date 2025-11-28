package clock

import (
	"testing"
	"time"
)

func TestRealClock(t *testing.T) {
	clk := NewReal()

	before := time.Now()
	now := clk.Now()
	after := time.Now()

	if now.Before(before) || now.After(after) {
		t.Errorf("Real clock time %v not between %v and %v", now, before, after)
	}

	start := time.Now()
	duration := 50 * time.Millisecond
	clk.Sleep(duration)
	elapsed := time.Since(start)

	if elapsed < duration {
		t.Errorf("Expected to sleep at least %v, but only slept %v", duration, elapsed)
	}
}

func TestMockClockNow(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := NewMock(startTime)

	now := clk.Now()
	if !now.Equal(startTime) {
		t.Errorf("Expected mock time %v, got %v", startTime, now)
	}
}

func TestMockClockSleep(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := NewMock(startTime)

	duration := 2 * time.Hour
	clk.Sleep(duration)

	expectedTime := startTime.Add(duration)
	now := clk.Now()

	if !now.Equal(expectedTime) {
		t.Errorf("Expected time %v after sleep, got %v", expectedTime, now)
	}
}

func TestMockClockMultipleSleeps(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := NewMock(startTime)

	durations := []time.Duration{
		1 * time.Hour,
		30 * time.Minute,
		2 * time.Hour,
		15 * time.Minute,
	}

	for _, d := range durations {
		clk.Sleep(d)
	}

	expectedTime := startTime.Add(3*time.Hour + 45*time.Minute)
	now := clk.Now()

	if !now.Equal(expectedTime) {
		t.Errorf("Expected time %v after multiple sleeps, got %v", expectedTime, now)
	}
}

func TestMockClockAdvance(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := NewMock(startTime)

	duration := 5 * time.Hour
	clk.Advance(duration)

	expectedTime := startTime.Add(duration)
	now := clk.Now()

	if !now.Equal(expectedTime) {
		t.Errorf("Expected time %v after advance, got %v", expectedTime, now)
	}
}

func TestMockClockSet(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := NewMock(startTime)

	newTime := time.Date(2024, 6, 15, 18, 30, 0, 0, time.UTC)
	clk.Set(newTime)

	now := clk.Now()
	if !now.Equal(newTime) {
		t.Errorf("Expected time %v after set, got %v", newTime, now)
	}
}

func TestMockClockZeroAllocation(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := NewMock(startTime)

	start := time.Now()
	for i := 0; i < 1000; i++ {
		clk.Sleep(time.Millisecond)
	}
	wallElapsed := time.Since(start)

	if wallElapsed > 100*time.Millisecond {
		t.Errorf("1000 mock sleeps took %v, expected near-instant", wallElapsed)
	}

	expectedMockTime := startTime.Add(1000 * time.Millisecond)
	if !clk.Now().Equal(expectedMockTime) {
		t.Errorf("Expected mock time %v, got %v", expectedMockTime, clk.Now())
	}

	t.Logf("1000 mock sleeps completed in %v wall time", wallElapsed)
}

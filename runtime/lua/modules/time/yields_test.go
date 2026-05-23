// SPDX-License-Identifier: MPL-2.0

package time

import (
	"sync"
	"sync/atomic"
	"testing"
	stdtime "time"

	lua "github.com/wippyai/go-lua"
	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

// Yield-pool / ToCommand tests for the router-tagged time yields.
//
// After the ephemeral channel router migration the yields carry
// (ChID, Epoch, GenRef) and the corresponding clock commands target
// engine.TopicEphemeral with a FireBuilder closure. Stop yields use the
// (TargetPID, Epoch, ChID) variants. The tests below exercise the new
// shape and confirm sync.Pool reuse still works.

func TestSleepYieldPool(t *testing.T) {
	y1 := acquireSleepYield(stdtime.Second)
	if y1.Duration != stdtime.Second {
		t.Errorf("expected Duration=%v, got %v", stdtime.Second, y1.Duration)
	}

	ReleaseSleepYield(y1)

	y2 := acquireSleepYield(stdtime.Millisecond)
	if y2.Duration != stdtime.Millisecond {
		t.Errorf("expected Duration=%v, got %v", stdtime.Millisecond, y2.Duration)
	}
	ReleaseSleepYield(y2)
}

func TestSleepYieldToCommand(t *testing.T) {
	y := acquireSleepYield(5 * stdtime.Millisecond)
	defer ReleaseSleepYield(y)

	cmd := y.ToCommand()
	sleepCmd, ok := cmd.(clockapi.SleepCmd)
	if !ok {
		t.Fatalf("expected clockapi.SleepCmd, got %T", cmd)
	}

	if sleepCmd.Duration != 5*stdtime.Millisecond {
		t.Errorf("expected Duration=5ms, got %v", sleepCmd.Duration)
	}
}

func TestSleepYieldLValueInterface(t *testing.T) {
	y := acquireSleepYield(stdtime.Nanosecond)
	defer ReleaseSleepYield(y)

	if y.String() != "<sleep_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}

	if y.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", y.Type())
	}
}

func TestSleepYieldPoolConcurrent(t *testing.T) {
	const goroutines = 100
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				d := stdtime.Duration(id*1000+i) * stdtime.Nanosecond
				y := acquireSleepYield(d)
				if y.Duration != d {
					t.Errorf("goroutine %d iter %d: expected %v, got %v", id, i, d, y.Duration)
				}
				cmd := y.ToCommand()
				if sleepCmd, ok := cmd.(clockapi.SleepCmd); !ok || sleepCmd.Duration != d {
					t.Errorf("goroutine %d iter %d: command conversion failed", id, i)
				}
				ReleaseSleepYield(y)
			}
		}(g)
	}

	wg.Wait()
}

func TestTimerStartYieldPool(t *testing.T) {
	ch := engine.NewChannel(1)
	p := pid.PID{}
	var gen atomic.Uint64

	y1 := acquireTimerStartYield(stdtime.Second, ch, p, 7, 3, &gen)
	if y1.Duration != stdtime.Second {
		t.Errorf("expected Duration=%v, got %v", stdtime.Second, y1.Duration)
	}
	if y1.ChID != 7 || y1.Epoch != 3 || y1.GenRef != &gen {
		t.Errorf("expected (ChID=7,Epoch=3,GenRef=%p), got (%d,%d,%p)", &gen, y1.ChID, y1.Epoch, y1.GenRef)
	}

	ReleaseTimerStartYield(y1)
	if y1.ChID != 0 || y1.Epoch != 0 || y1.GenRef != nil {
		t.Errorf("release should zero ChID/Epoch/GenRef, got (%d,%d,%p)", y1.ChID, y1.Epoch, y1.GenRef)
	}

	y2 := acquireTimerStartYield(stdtime.Millisecond, ch, p, 11, 5, &gen)
	if y2.Duration != stdtime.Millisecond {
		t.Errorf("expected Duration=%v, got %v", stdtime.Millisecond, y2.Duration)
	}
	ReleaseTimerStartYield(y2)
}

func TestTimerStartYieldToCommand(t *testing.T) {
	ch := engine.NewChannel(1)
	p := pid.PID{Node: "n", Host: "h", UniqID: "u"}
	var gen atomic.Uint64
	gen.Store(0)

	y := acquireTimerStartYield(50*stdtime.Millisecond, ch, p, 9, 2, &gen)
	defer ReleaseTimerStartYield(y)

	cmd := y.ToCommand()
	timerCmd, ok := cmd.(clockapi.TimerStartCmd)
	if !ok {
		t.Fatalf("expected clockapi.TimerStartCmd, got %T", cmd)
	}

	if timerCmd.Duration != 50*stdtime.Millisecond {
		t.Errorf("expected Duration=50ms, got %v", timerCmd.Duration)
	}
	if timerCmd.Topic != engine.TopicEphemeral {
		t.Errorf("expected Topic=%q, got %q", engine.TopicEphemeral, timerCmd.Topic)
	}
	if timerCmd.ChID != 9 || timerCmd.Epoch != 2 {
		t.Errorf("expected (ChID=9,Epoch=2), got (%d,%d)", timerCmd.ChID, timerCmd.Epoch)
	}
	if timerCmd.GenRef != &gen {
		t.Errorf("GenRef should propagate to the command")
	}
	if timerCmd.Build == nil {
		t.Error("Build should be set for router timers")
	}
}

func TestTimerStopYieldPool(t *testing.T) {
	p := pid.PID{Node: "n"}
	y1 := acquireTimerStopYield(p, 1, 10)
	if y1.ChID != 10 || y1.Epoch != 1 || y1.PID != p {
		t.Errorf("unexpected fields: %+v", y1)
	}

	ReleaseTimerStopYield(y1)
	if y1.ChID != 0 || y1.Epoch != 0 {
		t.Errorf("release should zero fields, got chID=%d epoch=%d", y1.ChID, y1.Epoch)
	}

	y2 := acquireTimerStopYield(p, 2, 20)
	if y2.ChID != 20 || y2.Epoch != 2 {
		t.Errorf("unexpected fields after re-acquire: %+v", y2)
	}
	ReleaseTimerStopYield(y2)
}

func TestTimerStopYieldToCommand(t *testing.T) {
	p := pid.PID{Node: "n"}
	y := acquireTimerStopYield(p, 7, 456)
	defer ReleaseTimerStopYield(y)

	cmd := y.ToCommand()
	stopCmd, ok := cmd.(clockapi.TimerStopByChIDCmd)
	if !ok {
		t.Fatalf("expected clockapi.TimerStopByChIDCmd, got %T", cmd)
	}

	if stopCmd.ChID != 456 || stopCmd.Epoch != 7 || stopCmd.TargetPID != p {
		t.Errorf("unexpected command: %+v", stopCmd)
	}
}

func TestTimerResetYieldPool(t *testing.T) {
	y1 := acquireTimerResetYield(42, stdtime.Second)
	if y1.TimerID != 42 {
		t.Errorf("expected TimerID=42, got %v", y1.TimerID)
	}
	if y1.Duration != stdtime.Second {
		t.Errorf("expected Duration=%v, got %v", stdtime.Second, y1.Duration)
	}

	ReleaseTimerResetYield(y1)

	y2 := acquireTimerResetYield(99, stdtime.Millisecond)
	if y2.TimerID != 99 {
		t.Errorf("expected TimerID=99, got %v", y2.TimerID)
	}
	if y2.Duration != stdtime.Millisecond {
		t.Errorf("expected Duration=%v, got %v", stdtime.Millisecond, y2.Duration)
	}
	ReleaseTimerResetYield(y2)
}

func TestTimerResetYieldToCommand(t *testing.T) {
	y := acquireTimerResetYield(123, 50*stdtime.Millisecond)
	defer ReleaseTimerResetYield(y)

	cmd := y.ToCommand()
	resetCmd, ok := cmd.(clockapi.TimerResetCmd)
	if !ok {
		t.Fatalf("expected clockapi.TimerResetCmd, got %T", cmd)
	}

	if resetCmd.TimerID != 123 {
		t.Errorf("expected TimerID=123, got %v", resetCmd.TimerID)
	}
	if resetCmd.Duration != 50*stdtime.Millisecond {
		t.Errorf("expected Duration=50ms, got %v", resetCmd.Duration)
	}
}

func TestTickerStartYieldPool(t *testing.T) {
	ch := engine.NewChannel(1)
	p := pid.PID{}
	var gen atomic.Uint64

	y1 := acquireTickerStartYield(stdtime.Second, ch, p, 13, 4, &gen)
	if y1.Duration != stdtime.Second {
		t.Errorf("expected Duration=%v, got %v", stdtime.Second, y1.Duration)
	}
	if y1.ChID != 13 || y1.Epoch != 4 || y1.GenRef != &gen {
		t.Errorf("unexpected fields: %+v", y1)
	}

	ReleaseTickerStartYield(y1)
	if y1.ChID != 0 || y1.Epoch != 0 || y1.GenRef != nil {
		t.Errorf("release should zero fields")
	}

	y2 := acquireTickerStartYield(stdtime.Millisecond, ch, p, 14, 4, &gen)
	if y2.Duration != stdtime.Millisecond {
		t.Errorf("expected Duration=%v, got %v", stdtime.Millisecond, y2.Duration)
	}
	ReleaseTickerStartYield(y2)
}

func TestTickerStartYieldToCommand(t *testing.T) {
	ch := engine.NewChannel(1)
	p := pid.PID{Node: "n"}
	var gen atomic.Uint64

	y := acquireTickerStartYield(100*stdtime.Millisecond, ch, p, 21, 6, &gen)
	defer ReleaseTickerStartYield(y)

	cmd := y.ToCommand()
	startCmd, ok := cmd.(clockapi.TickerStartCmd)
	if !ok {
		t.Fatalf("expected clockapi.TickerStartCmd, got %T", cmd)
	}

	if startCmd.Duration != 100*stdtime.Millisecond {
		t.Errorf("expected Duration=100ms, got %v", startCmd.Duration)
	}
	if startCmd.Topic != engine.TopicEphemeral {
		t.Errorf("expected Topic=%q, got %q", engine.TopicEphemeral, startCmd.Topic)
	}
	if startCmd.ChID != 21 || startCmd.Epoch != 6 {
		t.Errorf("unexpected (ChID,Epoch): (%d,%d)", startCmd.ChID, startCmd.Epoch)
	}
	if startCmd.Build == nil {
		t.Error("Build should be set for router tickers")
	}
}

func TestTickerStopYieldPool(t *testing.T) {
	p := pid.PID{Node: "n"}
	y1 := acquireTickerStopYield(p, 1, 10)
	if y1.ChID != 10 || y1.Epoch != 1 {
		t.Errorf("unexpected fields: %+v", y1)
	}

	ReleaseTickerStopYield(y1)
	if y1.ChID != 0 || y1.Epoch != 0 {
		t.Error("release should zero fields")
	}

	y2 := acquireTickerStopYield(p, 3, 20)
	if y2.ChID != 20 || y2.Epoch != 3 {
		t.Errorf("unexpected fields after re-acquire: %+v", y2)
	}
	ReleaseTickerStopYield(y2)
}

func TestTickerStopYieldToCommand(t *testing.T) {
	p := pid.PID{Node: "n"}
	y := acquireTickerStopYield(p, 9, 456)
	defer ReleaseTickerStopYield(y)

	cmd := y.ToCommand()
	stopCmd, ok := cmd.(clockapi.TickerStopByChIDCmd)
	if !ok {
		t.Fatalf("expected clockapi.TickerStopByChIDCmd, got %T", cmd)
	}

	if stopCmd.ChID != 456 || stopCmd.Epoch != 9 || stopCmd.TargetPID != p {
		t.Errorf("unexpected command: %+v", stopCmd)
	}
}

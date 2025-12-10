package time

import (
	"sync"
	"testing"
	stdtime "time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

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
	pid := relay.PID{}
	topic := "timer-test"

	y1 := acquireTimerStartYield(stdtime.Second, ch, pid, topic)
	if y1.Duration != stdtime.Second {
		t.Errorf("expected Duration=%v, got %v", stdtime.Second, y1.Duration)
	}

	ReleaseTimerStartYield(y1)

	y2 := acquireTimerStartYield(stdtime.Millisecond, ch, pid, topic)
	if y2.Duration != stdtime.Millisecond {
		t.Errorf("expected Duration=%v, got %v", stdtime.Millisecond, y2.Duration)
	}
	ReleaseTimerStartYield(y2)
}

func TestTimerStartYieldToCommand(t *testing.T) {
	ch := engine.NewChannel(1)
	pid := relay.PID{}
	topic := "timer-test-topic"

	y := acquireTimerStartYield(50*stdtime.Millisecond, ch, pid, topic)
	defer ReleaseTimerStartYield(y)

	cmd := y.ToCommand()
	timerCmd, ok := cmd.(clockapi.TimerStartCmd)
	if !ok {
		t.Fatalf("expected clockapi.TimerStartCmd, got %T", cmd)
	}

	if timerCmd.Duration != 50*stdtime.Millisecond {
		t.Errorf("expected Duration=50ms, got %v", timerCmd.Duration)
	}
	if timerCmd.Topic != topic {
		t.Errorf("expected Topic=%q, got %q", topic, timerCmd.Topic)
	}
}

func TestTimerStopYieldPool(t *testing.T) {
	y1 := acquireTimerStopYield(10)
	if y1.TimerID != 10 {
		t.Errorf("expected TimerID=10, got %v", y1.TimerID)
	}

	ReleaseTimerStopYield(y1)

	y2 := acquireTimerStopYield(20)
	if y2.TimerID != 20 {
		t.Errorf("expected TimerID=20, got %v", y2.TimerID)
	}
	ReleaseTimerStopYield(y2)
}

func TestTimerStopYieldToCommand(t *testing.T) {
	y := acquireTimerStopYield(456)
	defer ReleaseTimerStopYield(y)

	cmd := y.ToCommand()
	stopCmd, ok := cmd.(clockapi.TimerStopCmd)
	if !ok {
		t.Fatalf("expected clockapi.TimerStopCmd, got %T", cmd)
	}

	if stopCmd.TimerID != 456 {
		t.Errorf("expected TimerID=456, got %v", stopCmd.TimerID)
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
	pid := relay.PID{}
	topic := "test"

	y1 := acquireTickerStartYield(stdtime.Second, ch, pid, topic)
	if y1.Duration != stdtime.Second {
		t.Errorf("expected Duration=%v, got %v", stdtime.Second, y1.Duration)
	}

	ReleaseTickerStartYield(y1)

	y2 := acquireTickerStartYield(stdtime.Millisecond, ch, pid, topic)
	if y2.Duration != stdtime.Millisecond {
		t.Errorf("expected Duration=%v, got %v", stdtime.Millisecond, y2.Duration)
	}
	ReleaseTickerStartYield(y2)
}

func TestTickerStartYieldToCommand(t *testing.T) {
	ch := engine.NewChannel(1)
	pid := relay.PID{}
	topic := "test-topic"

	y := acquireTickerStartYield(100*stdtime.Millisecond, ch, pid, topic)
	defer ReleaseTickerStartYield(y)

	cmd := y.ToCommand()
	startCmd, ok := cmd.(clockapi.TickerStartCmd)
	if !ok {
		t.Fatalf("expected clockapi.TickerStartCmd, got %T", cmd)
	}

	if startCmd.Duration != 100*stdtime.Millisecond {
		t.Errorf("expected Duration=100ms, got %v", startCmd.Duration)
	}
	if startCmd.Topic != topic {
		t.Errorf("expected Topic=%q, got %q", topic, startCmd.Topic)
	}
}

func TestTickerStopYieldPool(t *testing.T) {
	y1 := acquireTickerStopYield(10)
	if y1.TickerID != 10 {
		t.Errorf("expected TickerID=10, got %v", y1.TickerID)
	}

	ReleaseTickerStopYield(y1)

	y2 := acquireTickerStopYield(20)
	if y2.TickerID != 20 {
		t.Errorf("expected TickerID=20, got %v", y2.TickerID)
	}
	ReleaseTickerStopYield(y2)
}

func TestTickerStopYieldToCommand(t *testing.T) {
	y := acquireTickerStopYield(456)
	defer ReleaseTickerStopYield(y)

	cmd := y.ToCommand()
	stopCmd, ok := cmd.(clockapi.TickerStopCmd)
	if !ok {
		t.Fatalf("expected clockapi.TickerStopCmd, got %T", cmd)
	}

	if stopCmd.TickerID != 456 {
		t.Errorf("expected TickerID=456, got %v", stopCmd.TickerID)
	}
}

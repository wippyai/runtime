package engine2

import (
	"context"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	clockapi "github.com/wippyai/runtime/api/dispatcher/clock"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
)

func TestSleepYieldPool(t *testing.T) {
	// Test acquire/release cycle
	y1 := acquireSleepYield(time.Second)
	if y1.Duration != time.Second {
		t.Errorf("expected Duration=%v, got %v", time.Second, y1.Duration)
	}

	ReleaseSleepYield(y1)

	// After release, the same object should be reused
	y2 := acquireSleepYield(time.Millisecond)
	if y2.Duration != time.Millisecond {
		t.Errorf("expected Duration=%v, got %v", time.Millisecond, y2.Duration)
	}
	ReleaseSleepYield(y2)
}

func TestSleepYieldToCommand(t *testing.T) {
	y := acquireSleepYield(5 * time.Millisecond)
	defer ReleaseSleepYield(y)

	cmd := y.ToCommand()
	sleepCmd, ok := cmd.(clockapi.SleepCmd)
	if !ok {
		t.Fatalf("expected clockapi.SleepCmd, got %T", cmd)
	}

	if sleepCmd.Duration != 5*time.Millisecond {
		t.Errorf("expected Duration=5ms, got %v", sleepCmd.Duration)
	}
}

func TestSleepYieldLValueInterface(t *testing.T) {
	y := acquireSleepYield(time.Nanosecond)
	defer ReleaseSleepYield(y)

	if y.String() != "<sleep_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}

	if y.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", y.Type())
	}
}

func TestConvertYieldToCommand(t *testing.T) {
	// Test with SleepYield
	y := acquireSleepYield(100 * time.Microsecond)
	cmd := ConvertYieldToCommand(y)
	if cmd == nil {
		t.Error("expected non-nil command")
	}
	if _, ok := cmd.(clockapi.SleepCmd); !ok {
		t.Errorf("expected clockapi.SleepCmd, got %T", cmd)
	}
	ReleaseSleepYield(y)

	// Test with non-convertible value
	cmd = ConvertYieldToCommand(lua.LNumber(42))
	if cmd != nil {
		t.Errorf("expected nil for non-convertible value, got %v", cmd)
	}

	// Test with nil
	cmd = ConvertYieldToCommand(lua.LNil)
	if cmd != nil {
		t.Errorf("expected nil for LNil, got %v", cmd)
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
				d := time.Duration(id*1000+i) * time.Nanosecond
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

func TestTimeSleepIntegration(t *testing.T) {
	script := `
		time.sleep(time.MILLISECOND)
		return "done"
	`

	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(
		WithProto(proto),
		WithModuleBinder(BindTimeSleep),
	)
	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// First step should yield with sleep command
	result, err := proc.Step(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.StepContinue {
		t.Fatalf("expected StepContinue, got %v", result.Status)
	}
	if result.YieldCount() != 1 {
		t.Fatalf("expected 1 yield, got %d", result.YieldCount())
	}

	// Check yield is SleepCmd
	yields := result.GetYields()
	if len(yields) != 1 {
		t.Fatalf("expected 1 yield command, got %d", len(yields))
	}
	sleepCmd, ok := yields[0].(clockapi.SleepCmd)
	if !ok {
		t.Fatalf("expected clockapi.SleepCmd, got %T", yields[0])
	}
	if sleepCmd.Duration != time.Millisecond {
		t.Errorf("expected 1ms, got %v", sleepCmd.Duration)
	}

	// Resume and complete
	result, err = proc.Step(&scheduler.YieldResults{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.StepDone {
		t.Fatalf("expected StepDone, got %v", result.Status)
	}
}

func TestTimeSleepMultipleYields(t *testing.T) {
	script := `
		for i = 1, 5 do
			time.sleep(time.NANOSECOND)
		end
		return "done"
	`

	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(
		WithProto(proto),
		WithModuleBinder(BindTimeSleep),
	)
	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// First step runs script until first yield
	result, err := proc.Step(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.StepContinue {
		t.Fatalf("expected StepContinue, got %v", result.Status)
	}

	// Run through remaining yield cycles (resume triggers next yield or completion)
	for i := 0; i < 5; i++ {
		result, err = proc.Step(&scheduler.YieldResults{})
		if err != nil {
			t.Fatalf("iteration %d resume: %v", i, err)
		}

		if result.Status == scheduler.StepDone {
			// Script completed after last resume
			return
		}

		if result.Status != scheduler.StepContinue {
			t.Fatalf("iteration %d: expected StepContinue or StepDone, got %v", i, result.Status)
		}
	}

	t.Fatal("script did not complete after 5 yield cycles")
}

func TestTimeNowYield(t *testing.T) {
	script := `
		local now = time.now()
		return now
	`

	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(
		WithProto(proto),
		WithModuleBinder(BindTimeSleep),
	)
	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// First step should yield with now command
	result, err := proc.Step(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.StepContinue {
		t.Fatalf("expected StepContinue, got %v", result.Status)
	}

	// Check yield is NowCmd
	yields := result.GetYields()
	if len(yields) != 1 {
		t.Fatalf("expected 1 yield, got %d", len(yields))
	}
	_, ok := yields[0].(clockapi.NowCmd)
	if !ok {
		t.Fatalf("expected clockapi.NowCmd, got %T", yields[0])
	}
}

func TestTimeTimerYield(t *testing.T) {
	script := `
		local fireTime = time.timer(100 * time.MILLISECOND)
		return fireTime
	`

	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(
		WithProto(proto),
		WithModuleBinder(BindTimeSleep),
	)
	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// First step should yield with timer command
	result, err := proc.Step(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.StepContinue {
		t.Fatalf("expected StepContinue, got %v", result.Status)
	}

	// Check yield is TimerCmd
	yields := result.GetYields()
	if len(yields) != 1 {
		t.Fatalf("expected 1 yield, got %d", len(yields))
	}
	timerCmd, ok := yields[0].(clockapi.TimerCmd)
	if !ok {
		t.Fatalf("expected clockapi.TimerCmd, got %T", yields[0])
	}
	if timerCmd.Duration != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", timerCmd.Duration)
	}
}

func TestTimerYieldPool(t *testing.T) {
	y1 := acquireTimerYield(time.Second)
	if y1.Duration != time.Second {
		t.Errorf("expected Duration=%v, got %v", time.Second, y1.Duration)
	}

	ReleaseTimerYield(y1)

	y2 := acquireTimerYield(time.Millisecond)
	if y2.Duration != time.Millisecond {
		t.Errorf("expected Duration=%v, got %v", time.Millisecond, y2.Duration)
	}
	ReleaseTimerYield(y2)
}

func TestTimerYieldToCommand(t *testing.T) {
	y := acquireTimerYield(50 * time.Millisecond)
	defer ReleaseTimerYield(y)

	cmd := y.ToCommand()
	timerCmd, ok := cmd.(clockapi.TimerCmd)
	if !ok {
		t.Fatalf("expected clockapi.TimerCmd, got %T", cmd)
	}

	if timerCmd.Duration != 50*time.Millisecond {
		t.Errorf("expected Duration=50ms, got %v", timerCmd.Duration)
	}
}

func TestNowYieldSingleton(t *testing.T) {
	// NowYield should use singleton pattern
	cmd1 := nowYieldSingleton.ToCommand()
	cmd2 := nowYieldSingleton.ToCommand()

	_, ok1 := cmd1.(clockapi.NowCmd)
	_, ok2 := cmd2.(clockapi.NowCmd)
	if !ok1 || !ok2 {
		t.Fatalf("expected clockapi.NowCmd")
	}

	if nowYieldSingleton.String() != "<now_yield>" {
		t.Errorf("unexpected String(): %s", nowYieldSingleton.String())
	}
}

func TestTickerStartYieldPool(t *testing.T) {
	y1 := acquireTickerStartYield(time.Second)
	if y1.Duration != time.Second {
		t.Errorf("expected Duration=%v, got %v", time.Second, y1.Duration)
	}

	ReleaseTickerStartYield(y1)

	y2 := acquireTickerStartYield(time.Millisecond)
	if y2.Duration != time.Millisecond {
		t.Errorf("expected Duration=%v, got %v", time.Millisecond, y2.Duration)
	}
	ReleaseTickerStartYield(y2)
}

func TestTickerStartYieldToCommand(t *testing.T) {
	y := acquireTickerStartYield(100 * time.Millisecond)
	defer ReleaseTickerStartYield(y)

	cmd := y.ToCommand()
	startCmd, ok := cmd.(clockapi.TickerStartCmd)
	if !ok {
		t.Fatalf("expected clockapi.TickerStartCmd, got %T", cmd)
	}

	if startCmd.Duration != 100*time.Millisecond {
		t.Errorf("expected Duration=100ms, got %v", startCmd.Duration)
	}
}

func TestTickerNextYieldPool(t *testing.T) {
	y1 := acquireTickerNextYield(42)
	if y1.TickerID != 42 {
		t.Errorf("expected TickerID=42, got %v", y1.TickerID)
	}

	ReleaseTickerNextYield(y1)

	y2 := acquireTickerNextYield(99)
	if y2.TickerID != 99 {
		t.Errorf("expected TickerID=99, got %v", y2.TickerID)
	}
	ReleaseTickerNextYield(y2)
}

func TestTickerNextYieldToCommand(t *testing.T) {
	y := acquireTickerNextYield(123)
	defer ReleaseTickerNextYield(y)

	cmd := y.ToCommand()
	nextCmd, ok := cmd.(clockapi.TickerNextCmd)
	if !ok {
		t.Fatalf("expected clockapi.TickerNextCmd, got %T", cmd)
	}

	if nextCmd.TickerID != 123 {
		t.Errorf("expected TickerID=123, got %v", nextCmd.TickerID)
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

func TestConvertYieldToCommandStreaming(t *testing.T) {
	// Test TickerStartYield
	startYield := acquireTickerStartYield(time.Second)
	startCmd := ConvertYieldToCommand(startYield)
	if _, ok := startCmd.(clockapi.TickerStartCmd); !ok {
		t.Errorf("expected clockapi.TickerStartCmd, got %T", startCmd)
	}
	ReleaseTickerStartYield(startYield)

	// Test TickerNextYield
	nextYield := acquireTickerNextYield(1)
	nextCmd := ConvertYieldToCommand(nextYield)
	if _, ok := nextCmd.(clockapi.TickerNextCmd); !ok {
		t.Errorf("expected clockapi.TickerNextCmd, got %T", nextCmd)
	}
	ReleaseTickerNextYield(nextYield)

	// Test TickerStopYield
	stopYield := acquireTickerStopYield(1)
	stopCmd := ConvertYieldToCommand(stopYield)
	if _, ok := stopCmd.(clockapi.TickerStopCmd); !ok {
		t.Errorf("expected clockapi.TickerStopCmd, got %T", stopCmd)
	}
	ReleaseTickerStopYield(stopYield)
}

func TestStreamReadYieldPool(t *testing.T) {
	y1 := acquireStreamReadYield(42, 1024)
	if y1.StreamID != 42 {
		t.Errorf("expected StreamID=42, got %v", y1.StreamID)
	}
	if y1.Size != 1024 {
		t.Errorf("expected Size=1024, got %v", y1.Size)
	}

	ReleaseStreamReadYield(y1)

	y2 := acquireStreamReadYield(99, 2048)
	if y2.StreamID != 99 {
		t.Errorf("expected StreamID=99, got %v", y2.StreamID)
	}
	ReleaseStreamReadYield(y2)
}

func TestStreamReadYieldToCommand(t *testing.T) {
	y := acquireStreamReadYield(123, 4096)
	defer ReleaseStreamReadYield(y)

	cmd := y.ToCommand()
	if cmd.CmdID() != 50 {
		t.Errorf("expected CmdID=50, got %v", cmd.CmdID())
	}

	if y.String() != "<stream_read_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestStreamCloseYieldPool(t *testing.T) {
	y1 := acquireStreamCloseYield(10)
	if y1.StreamID != 10 {
		t.Errorf("expected StreamID=10, got %v", y1.StreamID)
	}

	ReleaseStreamCloseYield(y1)

	y2 := acquireStreamCloseYield(20)
	if y2.StreamID != 20 {
		t.Errorf("expected StreamID=20, got %v", y2.StreamID)
	}
	ReleaseStreamCloseYield(y2)
}

func TestStreamCloseYieldToCommand(t *testing.T) {
	y := acquireStreamCloseYield(456)
	defer ReleaseStreamCloseYield(y)

	cmd := y.ToCommand()
	if cmd.CmdID() != 51 {
		t.Errorf("expected CmdID=51, got %v", cmd.CmdID())
	}

	if y.String() != "<stream_close_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestBindStream(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	BindStream(l)

	// Verify __stream_new helper is registered
	fn := l.GetGlobal("__stream_new")
	if fn == lua.LNil {
		t.Fatal("__stream_new not registered")
	}
}

func TestStreamMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	BindStream(l)

	// Create a stream via __stream_new
	err := l.DoString(`
		local stream = __stream_new(42)
		if stream == nil then
			error("stream is nil")
		end
	`)
	if err != nil {
		t.Fatalf("DoString failed: %v", err)
	}
}

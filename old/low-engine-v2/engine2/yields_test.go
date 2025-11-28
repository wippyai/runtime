package engine2

import (
	"context"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/low-engine-v2/clock"
	"github.com/wippyai/runtime/low-engine-v2/scheduler"
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
	sleepCmd, ok := cmd.(clock.SleepCmd)
	if !ok {
		t.Fatalf("expected clock.SleepCmd, got %T", cmd)
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
	if _, ok := cmd.(clock.SleepCmd); !ok {
		t.Errorf("expected clock.SleepCmd, got %T", cmd)
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
				if sleepCmd, ok := cmd.(clock.SleepCmd); !ok || sleepCmd.Duration != d {
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
	if err := proc.Start(ctx, nil); err != nil {
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
	if result.YieldCount != 1 {
		t.Fatalf("expected 1 yield, got %d", result.YieldCount)
	}

	// Check yield is SleepCmd
	yields := result.GetYields()
	if len(yields) != 1 {
		t.Fatalf("expected 1 yield command, got %d", len(yields))
	}
	sleepCmd, ok := yields[0].(clock.SleepCmd)
	if !ok {
		t.Fatalf("expected clock.SleepCmd, got %T", yields[0])
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
	if err := proc.Start(ctx, nil); err != nil {
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

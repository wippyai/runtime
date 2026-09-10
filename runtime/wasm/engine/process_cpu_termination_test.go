// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wazeroapi "github.com/tetratelabs/wazero/api"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	sysscheduler "github.com/wippyai/runtime/system/scheduler"
	actorsched "github.com/wippyai/runtime/system/scheduler/actor"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wat"
)

// Planned manifest contract VAL-W1-FAULT-CPU-TERMINATE:
// Stock Wazero termination terminates CPU-bound guest; dedicated worker
// is reported occupied until termination, with NO resumable preemption or fuel claim.

type testLifecycle struct {
	onStart    func(context.Context, pidapi.PID, process.Process)
	onComplete func(context.Context, pidapi.PID, *runtime.Result)
}

func (l *testLifecycle) OnStart(ctx context.Context, p pidapi.PID, proc process.Process) error {
	if l.onStart != nil {
		l.onStart(ctx, p, proc)
	}
	return nil
}

func (l *testLifecycle) OnComplete(ctx context.Context, p pidapi.PID, res *runtime.Result) {
	if l.onComplete != nil {
		l.onComplete(ctx, p, res)
	}
}

func TestActor_TerminateCPUBoundGuest(t *testing.T) {
	const childEnvKey = "WIPPY_CPU_TERMINATE_SUBPROCESS"
	if os.Getenv(childEnvKey) == "1" {
		t.Run("DirectProcessCancellation", testDirectProcessCancellation)
		t.Run("SchedulerDedicatedWorkerOccupiedUntilTermination", testSchedulerDedicatedWorkerOccupiedUntilTermination)
		return
	}
	// Every infinite-loop scenario runs inside the killable child, including
	// teardown: broken cancellation must not strand the parent package test.
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestActor_TerminateCPUBoundGuest$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), childEnvKey+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CPU actor termination child failed: %v (%v)\n%s", err, ctx.Err(), out)
	}
	t.Logf("CPU actor termination child completed:\n%s", out)
}

func testDirectProcessCancellation(t *testing.T) {
	ctx := context.Background()

	// Stock Wazero runtime with CloseOnContextDone enabled
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer rt.Close(ctx)

	// Entered host callback barrier: guest calls this before starting CPU loop
	enteredCh := make(chan struct{})
	var enteredOnce sync.Once
	err = rt.RegisterCoreFunc("env", "entered", nil, nil, func(_ context.Context, _ wazeroapi.Module, _ []uint64) {
		enteredOnce.Do(func() {
			close(enteredCh)
		})
	}, false)
	if err != nil {
		t.Fatalf("RegisterCoreFunc env:entered: %v", err)
	}

	freshDoneCh := make(chan struct{})
	var freshOnce sync.Once
	err = rt.RegisterCoreFunc("env", "fresh_done", nil, nil, func(_ context.Context, _ wazeroapi.Module, _ []uint64) {
		freshOnce.Do(func() {
			close(freshDoneCh)
		})
	}, false)
	if err != nil {
		t.Fatalf("RegisterCoreFunc env:fresh_done: %v", err)
	}

	// Tiny embedded WAT CPU-only loop with entered signal host callback
	cpuLoopWat := `(module
  (import "env" "entered" (func $entered))
  (func (export "run")
    (call $entered)
    (loop $l (br $l))
  )
)`
	cpuBytes, err := wat.Compile(cpuLoopWat)
	if err != nil {
		t.Fatalf("wat.Compile cpuLoopWat: %v", err)
	}
	cpuMod, err := rt.LoadWASM(ctx, cpuBytes, "run: func();")
	if err != nil {
		t.Fatalf("LoadWASM cpuMod: %v", err)
	}
	if err := cpuMod.Compile(ctx); err != nil {
		t.Fatalf("cpuMod.Compile: %v", err)
	}

	var cpuReleased atomic.Bool
	cpuProc := NewActorProcess(
		NewProcess(cpuMod, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil),
		actor.DefaultLimits(),
		func() { cpuReleased.Store(true) },
	)
	defer cpuProc.Close()

	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := cpuProc.Init(callCtx, "run", nil); err != nil {
		t.Fatalf("cpuProc.Init: %v", err)
	}

	stepErrCh := make(chan error, 1)
	stepDoneCh := make(chan struct{})

	go func() {
		defer close(stepDoneCh)
		var out process.StepOutput
		err := cpuProc.Step(nil, &out)
		stepErrCh <- err
	}()

	// 1. Await deterministic signal that guest entered CPU loop without sleep
	select {
	case <-enteredCh:
		t.Log("Direct: guest entered CPU loop")
	case <-time.After(3 * time.Second):
		t.Fatal("Direct: timed out waiting for enteredCh")
	}

	// 2. Cancel execution through actual supported mechanism (context cancellation)
	cancel()

	// 3. Assert Step goroutine exits in bounded time
	select {
	case <-stepDoneCh:
		t.Log("Direct: Step goroutine exited cleanly after cancellation")
	case <-time.After(3 * time.Second):
		t.Fatal("Direct: timed out waiting for Step goroutine to exit")
	}

	stepErr := <-stepErrCh
	if stepErr == nil {
		t.Fatal("Direct: expected error on cancellation, got nil")
	}
	t.Logf("Direct: Step returned termination error: %v", stepErr)

	// 4. Assert NO resumable preemption/fuel claim: attempting to resume Step fails
	var resumeOut process.StepOutput
	resumeErr := cpuProc.Step(nil, &resumeOut)
	if resumeErr == nil {
		t.Fatal("Direct: expected error on resume attempt of cancelled guest, got nil")
	}
	t.Logf("Direct: verified no resumable preemption: resume failed with %v", resumeErr)

	// 5. Assert resources release on close
	cpuProc.Close()
	if !cpuReleased.Load() {
		t.Fatal("Direct: expected releaseResources to be called")
	}

	// 6. Demonstrate a fresh actor still runs cleanly
	freshWat := `(module
  (import "env" "fresh_done" (func $fresh_done))
  (func (export "run")
    (call $fresh_done)
  )
)`
	freshBytes, err := wat.Compile(freshWat)
	if err != nil {
		t.Fatalf("wat.Compile freshWat: %v", err)
	}
	freshMod, err := rt.LoadWASM(ctx, freshBytes, "run: func();")
	if err != nil {
		t.Fatalf("LoadWASM freshMod: %v", err)
	}
	if err := freshMod.Compile(ctx); err != nil {
		t.Fatalf("freshMod.Compile: %v", err)
	}

	var freshReleased atomic.Bool
	freshProc := NewActorProcess(
		NewProcess(freshMod, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil),
		actor.DefaultLimits(),
		func() { freshReleased.Store(true) },
	)
	defer freshProc.Close()

	if err := freshProc.Init(ctx, "run", nil); err != nil {
		t.Fatalf("freshProc.Init: %v", err)
	}
	var freshOut process.StepOutput
	if err := freshProc.Step(nil, &freshOut); err != nil {
		t.Fatalf("freshProc.Step: %v", err)
	}
	select {
	case <-freshDoneCh:
		t.Log("Direct: fresh actor completed successfully")
	default:
		t.Fatal("Direct: fresh actor callback was not called")
	}
	freshProc.Close()
	if !freshReleased.Load() {
		t.Fatal("Direct: expected fresh actor releaseResources to be called")
	}
}

func testSchedulerDedicatedWorkerOccupiedUntilTermination(t *testing.T) {
	ctx := context.Background()

	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer rt.Close(ctx)

	enteredCh := make(chan struct{})
	var enteredOnce sync.Once
	err = rt.RegisterCoreFunc("env", "entered", nil, nil, func(_ context.Context, _ wazeroapi.Module, _ []uint64) {
		enteredOnce.Do(func() {
			close(enteredCh)
		})
	}, false)
	if err != nil {
		t.Fatalf("RegisterCoreFunc env:entered: %v", err)
	}

	freshDoneCh := make(chan struct{})
	var freshOnce sync.Once
	err = rt.RegisterCoreFunc("env", "fresh_done", nil, nil, func(_ context.Context, _ wazeroapi.Module, _ []uint64) {
		freshOnce.Do(func() {
			close(freshDoneCh)
		})
	}, false)
	if err != nil {
		t.Fatalf("RegisterCoreFunc env:fresh_done: %v", err)
	}

	cpuLoopWat := `(module
  (import "env" "entered" (func $entered))
  (func (export "run")
    (call $entered)
    (loop $l (br $l))
  )
)`
	cpuBytes, err := wat.Compile(cpuLoopWat)
	if err != nil {
		t.Fatalf("wat.Compile cpuLoopWat: %v", err)
	}
	cpuMod, err := rt.LoadWASM(ctx, cpuBytes, "run: func();")
	if err != nil {
		t.Fatalf("LoadWASM cpuMod: %v", err)
	}
	if err := cpuMod.Compile(ctx); err != nil {
		t.Fatalf("cpuMod.Compile: %v", err)
	}

	freshWat := `(module
  (import "env" "fresh_done" (func $fresh_done))
  (func (export "run")
    (call $fresh_done)
  )
)`
	freshBytes, err := wat.Compile(freshWat)
	if err != nil {
		t.Fatalf("wat.Compile freshWat: %v", err)
	}
	freshMod, err := rt.LoadWASM(ctx, freshBytes, "run: func();")
	if err != nil {
		t.Fatalf("LoadWASM freshMod: %v", err)
	}
	if err := freshMod.Compile(ctx); err != nil {
		t.Fatalf("freshMod.Compile: %v", err)
	}

	cpuReleased := make(chan struct{})
	cpuProc := NewActorProcess(
		NewProcess(cpuMod, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil),
		actor.DefaultLimits(),
		func() { close(cpuReleased) },
	)

	freshReleased := make(chan struct{})
	freshProc := NewActorProcess(
		NewProcess(freshMod, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil),
		actor.DefaultLimits(),
		func() { close(freshReleased) },
	)

	cpuCompletedCh := make(chan *runtime.Result, 1)
	freshCompletedCh := make(chan *runtime.Result, 1)

	cpuPID := pidapi.PID{Node: "local", Host: "actors", UniqID: "cpu-bound"}
	freshPID := pidapi.PID{Node: "local", Host: "actors", UniqID: "fresh"}

	lc := &testLifecycle{
		onComplete: func(_ context.Context, p pidapi.PID, res *runtime.Result) {
			switch p {
			case cpuPID:
				cpuCompletedCh <- res
			case freshPID:
				freshCompletedCh <- res
			}
		},
	}

	cmdRegistry := sysscheduler.NewRegistry()
	sched := actorsched.NewScheduler(cmdRegistry, actorsched.WithWorkers(1), actorsched.WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(ctx)

	// Submit CPU-bound actor
	_, err = sched.Submit(ctx, cpuPID, cpuProc, "run", nil)
	if err != nil {
		t.Fatalf("sched.Submit cpuPID: %v", err)
	}

	// 1. Await deterministic guest entry barrier (no sleep)
	select {
	case <-enteredCh:
		t.Log("Scheduler: CPU-bound guest entered CPU loop")
	case <-time.After(3 * time.Second):
		t.Fatal("Scheduler: timed out waiting for CPU guest to enter loop")
	}

	// 2. Submit fresh actor while CPU actor is running.
	// Since there is only 1 worker and it is occupied by the CPU-bound guest,
	// the fresh actor must NOT run yet (no resumable preemption/fuel timeslicing).
	_, err = sched.Submit(ctx, freshPID, freshProc, "run", nil)
	if err != nil {
		t.Fatalf("sched.Submit freshPID: %v", err)
	}

	// Assert fresh actor does not run while worker is occupied
	select {
	case <-freshDoneCh:
		t.Fatal("Scheduler: fresh actor ran while single worker was occupied by CPU loop (unexpected preemption)")
	case <-time.After(100 * time.Millisecond):
		t.Log("Scheduler: verified dedicated worker remains occupied by CPU-bound guest; fresh actor is queued")
	}

	// 3. Terminate CPU-bound actor through supported scheduler mechanism
	err = sched.Terminate(cpuPID)
	if err != nil {
		t.Fatalf("sched.Terminate: %v", err)
	}

	// 4. Assert CPU-bound actor completes and Step terminates
	var cpuRes *runtime.Result
	select {
	case cpuRes = <-cpuCompletedCh:
		t.Logf("Scheduler: CPU actor terminated with result: %v", cpuRes.Error)
	case <-time.After(3 * time.Second):
		t.Fatal("Scheduler: timed out waiting for CPU actor to terminate")
	}

	if cpuRes.Error == nil {
		t.Fatal("Scheduler: expected non-nil error for terminated CPU actor")
	}

	// 5. Assert dedicated worker is freed and fresh actor now executes to completion
	select {
	case <-freshDoneCh:
		t.Log("Scheduler: fresh actor host callback confirmed execution")
	case <-time.After(3 * time.Second):
		t.Fatal("Scheduler: timed out waiting for fresh actor to execute after CPU actor termination")
	}

	select {
	case freshRes := <-freshCompletedCh:
		if freshRes.Error != nil {
			t.Fatalf("Scheduler: fresh actor completed with unexpected error: %v", freshRes.Error)
		}
		t.Log("Scheduler: fresh actor completed successfully")
	case <-time.After(3 * time.Second):
		t.Fatal("Scheduler: timed out waiting for fresh actor completion result")
	}

	// Completion notifications precede process Close; wait for both releases.
	select {
	case <-cpuReleased:
	case <-time.After(3 * time.Second):
		t.Fatal("Scheduler: timed out waiting for CPU actor resource release")
	}

	select {
	case <-freshReleased:
	case <-time.After(3 * time.Second):
		t.Fatal("Scheduler: timed out waiting for fresh actor resource release")
	}

	// 7. Verify process is evicted from scheduler lookup
	err = sched.Send(&relay.Package{Target: cpuPID})
	if !errors.Is(err, process.ErrProcessNotFound) {
		t.Fatalf("Scheduler: expected ErrProcessNotFound after termination, got %v", err)
	}
}

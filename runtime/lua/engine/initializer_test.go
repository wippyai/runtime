// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/process"
)

// An initializer that fails is retried by the next execution of the process;
// completion callbacks run once, after the last initializer succeeds.
func TestFailedInitializerIsRetriedByNextExecution(t *testing.T) {
	var attempts, done, initialized int
	proc := mustNewProcess(t,
		WithModuleBinder(wrapBinder(func(l *lua.LState) {
			DeferInitializer(l, Initializer{
				Fn: l.NewFunction(func(l *lua.LState) int {
					attempts++
					if attempts == 1 {
						l.RaiseError("init failed")
					}
					l.Push(lua.LNumber(attempts))
					return 1
				}),
				Done: func(lua.LValue) { done++ },
			})
			OnInitialized(l, func() { initialized++ })
		})),
		WithScript(`return { main = function() return 1 end }`, "m.lua"))
	t.Cleanup(proc.Close)

	if err := proc.Init(frameContext(), "main", nil); err != nil {
		t.Fatal(err)
	}
	var output process.StepOutput
	if err := proc.Step(nil, &output); err == nil {
		t.Fatal("expected the failing initializer to fail the step")
	}
	if done != 0 || initialized != 0 {
		t.Fatalf("failed initializer completed: done=%d initialized=%d", done, initialized)
	}

	runProcess(t, proc, 1)
	if attempts != 2 || done != 1 || initialized != 1 {
		t.Fatalf("attempts=%d done=%d initialized=%d", attempts, done, initialized)
	}
}

// A missing method fails with a not-found error after the initializers
// completed, and a later execution does not run them again.
func TestMethodNotFoundAfterInitializers(t *testing.T) {
	var runs, done int
	proc := mustNewProcess(t,
		WithModuleBinder(wrapBinder(func(l *lua.LState) {
			DeferInitializer(l, Initializer{
				Fn:   l.NewFunction(func(*lua.LState) int { runs++; return 0 }),
				Done: func(lua.LValue) { done++ },
			})
		})),
		WithScript(`return { main = function() return 1 end }`, "m.lua"))
	t.Cleanup(proc.Close)

	if err := proc.Init(frameContext(), "missing", nil); err != nil {
		t.Fatal(err)
	}
	var output process.StepOutput
	requireMethodNotFound(t, proc.Step(nil, &output))
	if runs != 1 || done != 1 {
		t.Fatalf("initializer runs=%d done=%d, want 1 and 1", runs, done)
	}

	runProcess(t, proc, 1)
	if runs != 1 {
		t.Fatalf("completed initializer ran again: %d", runs)
	}
}

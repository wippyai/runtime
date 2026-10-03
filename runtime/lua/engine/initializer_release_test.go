// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
)

// Completed initializers and one-shot completion callbacks are not retained by
// the state that outlives them.
func TestCompletedInitializersAreReleased(t *testing.T) {
	var collected atomic.Int64
	probe := func() *finalizerProbe {
		p := &finalizerProbe{collected: &collected}
		runtime.SetFinalizer(p, func(p *finalizerProbe) { p.collected.Add(1) })
		return p
	}
	// The binder creates the captures itself: a binder that closed over them
	// would keep them alive through the process's factory.
	proc := mustNewProcess(t,
		WithModuleBinder(wrapBinder(func(l *lua.LState) {
			initProbe, doneProbe, callbackProbe := probe(), probe(), probe()
			DeferInitializer(l, Initializer{
				Fn:   l.NewFunction(func(*lua.LState) int { _ = initProbe; return 0 }),
				Done: func(lua.LValue) { _ = doneProbe },
			})
			OnInitialized(l, func() { _ = callbackProbe })
		})),
		WithScript(`return { main = function() return 1 end }`, "m.lua"))
	t.Cleanup(proc.Close)

	runProcess(t, proc, 1)

	deadline := time.Now().Add(5 * time.Second)
	for collected.Load() < 3 && time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	if got := collected.Load(); got != 3 {
		t.Fatalf("%d of 3 completed initializer captures were collected", got)
	}
}

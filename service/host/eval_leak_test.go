// SPDX-License-Identifier: MPL-2.0

package host

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	"go.uber.org/zap"
)

// frameProbe is a frame attachment that records its release, by frame
// reclamation or rollback.
type frameProbe struct {
	released atomic.Bool
}

func (p *frameProbe) Close() error    { p.released.Store(true); return nil }
func (p *frameProbe) Rollback() error { p.released.Store(true); return nil }

var probeKey = &ctxapi.Key{Name: "test.leak.probe"}

// probes hands one probe to every start the manager resolves.
type probes struct {
	all []*frameProbe
	mu  sync.Mutex
}

func (p *probes) resolve(context.Context, attrs.Attributes) ([]ctxapi.Pair, error) {
	probe := &frameProbe{}
	p.mu.Lock()
	p.all = append(p.all, probe)
	p.mu.Unlock()
	return []ctxapi.Pair{{Key: probeKey, Value: probe}}, nil
}

func (p *probes) unreleased() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, probe := range p.all {
		if !probe.released.Load() {
			n++
		}
	}
	return n
}

const (
	evalOK    = `return { main = function() return 1 end }`
	evalFail  = `return { main = function() error("boom") end }`
	evalSpin  = `return { main = function() while true do end end }`
	evalChild = `return { main = function() return 1 end }`
)

func TestEvalSpawnCyclesLeaveNothingBehind(t *testing.T) {
	env := newLeakEnv(t)
	env.slots = process.NewChildSlots(1 << 20)
	probed := &probes{}
	require.NoError(t, env.resolvers.Register("probe", 1, probed.resolve))

	modules := func() []*luaapi.ModuleDef { return nil }
	admitter := evalhost.NewAdmitter(evalhost.NewHost(zap.NewNop(), modules), env.manager,
		evalhost.WithSpawnHost("test:host"), evalhost.WithDetachedLifetime(20*time.Millisecond))

	type scenario struct {
		source string
		link   apihost.EvalLinkMode
		// ownerWaits keeps the owner running until the evals end.
		detach bool
	}
	scenarios := map[string]scenario{
		"owned success":     {source: evalOK},
		"owned error":       {source: evalFail},
		"owned spinning":    {source: evalSpin},
		"monitor success":   {source: evalOK, link: apihost.EvalLinkMonitorOnly},
		"monitor spinning":  {source: evalSpin, link: apihost.EvalLinkMonitorOnly},
		"detached success":  {source: evalOK, link: apihost.EvalLinkDetached, detach: true},
		"detached spinning": {source: evalSpin, link: apihost.EvalLinkDetached, detach: true},
	}

	var spawned atomic.Int64
	run := func(sc scenario) {
		owner := &leakProcess{mode: childDone}
		owner.init = func(ctx context.Context) error {
			env.scopes.Store(process.GetExecutionScope(ctx), struct{}{})
			parent, _ := apiruntime.GetFramePID(ctx)
			_, err := admitter.Spawn(ctx, apihost.EvalSpawnSpec{
				SourceCode: sc.source,
				Parent:     parent,
				LinkMode:   sc.link,
				Policy:     apihost.EvalPolicy{AllowDetached: sc.detach},
			})
			if err == nil {
				spawned.Add(1)
			}
			return err
		}
		env.started.Add(1)
		_, err := env.host.Run(env.ctx, &process.Start{
			HostID:    "test:host",
			Source:    registry.NewID("eval.program", "owner"),
			Context:   []ctxapi.Pair{process.ChildSlotsPair(env.slots)},
			Options:   attrs.Bag{},
			Admission: env.admission(owner),
		})
		require.NoError(t, err)
	}
	batch := func(n int) {
		for i := 0; i < n; i++ {
			for _, sc := range scenarios {
				run(sc)
			}
		}
		// every owner and every eval ends
		settle(t, func() bool { return env.completed.Load() == env.started.Load()+spawned.Load() }, "processes complete")
		settle(t, func() bool { return env.slots.InUse() == 0 }, "child slots released")
		settle(t, func() bool { return probed.unreleased() == 0 }, "frames released")
		env.scopes.Range(func(k, _ any) bool {
			require.Zero(t, k.(*process.ExecutionScope).Owned(), "owned registrations released")
			env.scopes.Delete(k)
			return true
		})
	}

	batch(10)
	goroutines := runtime.NumGoroutine()
	heap := heapInUse()
	for i := 0; i < 3; i++ {
		batch(40)
	}
	settle(t, func() bool { return runtime.NumGoroutine() <= goroutines+2 }, "goroutines stable")
	grown := int64(heapInUse()) - int64(heap)
	require.Less(t, grown, int64(8<<20), "heap grew by %d bytes", grown)
	_ = pid.PID{}
}

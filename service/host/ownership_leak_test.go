// SPDX-License-Identifier: MPL-2.0

package host

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	hostapi "github.com/wippyai/runtime/api/service/host"
	sysprocess "github.com/wippyai/runtime/system/process"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

type leakNode struct {
	hosts map[pid.HostID]relay.Receiver
	mu    sync.RWMutex
}

func (n *leakNode) ID() pid.NodeID { return "test-node" }
func (n *leakNode) GetHost(id pid.HostID) (relay.Receiver, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	h, ok := n.hosts[id]
	return h, ok
}
func (n *leakNode) RegisterHost(id pid.HostID, h relay.Receiver) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.hosts[id] = h
	return nil
}
func (n *leakNode) UnregisterHost(pid.HostID) {}
func (n *leakNode) Send(*relay.Package) error { return nil }
func (n *leakNode) Detach(pid.PID)            {}
func (n *leakNode) Attach(pid.PID, chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}

// frameCloser counts how many frames holding it were reclaimed.
type frameCloser struct{ closed atomic.Int64 }

func (c *frameCloser) Close() error {
	c.closed.Add(1)
	return nil
}

var leakCloserKey = &ctxapi.Key{Name: "test.leak.closer"}

type childMode int

const (
	childDone childMode = iota
	childFail
	childBlock
)

// leakProcess runs init in Init and ends according to mode.
type leakProcess struct {
	init func(ctx context.Context) error
	// afterInit fails the process after its init ran.
	afterInit error
	// startErr fails the process's lifecycle start.
	startErr error
	mode     childMode
}

func (p *leakProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	if p.init != nil {
		return p.init(ctx)
	}
	return nil
}

func (p *leakProcess) Step(_ []process.Event, out *process.StepOutput) error {
	switch p.mode {
	case childFail:
		return errors.New("boom")
	case childBlock:
		return nil
	}
	out.Done(nil)
	return nil
}

func (p *leakProcess) Close() {}

type leakLifecycle struct {
	host      *Host
	completed *atomic.Int64
}

func (l *leakLifecycle) OnStart(_ context.Context, _ pid.PID, p process.Process) error {
	if proc, ok := p.(*leakProcess); ok && proc.startErr != nil {
		return proc.startErr
	}
	return nil
}
func (l *leakLifecycle) OnComplete(ctx context.Context, p pid.PID, r *apiruntime.Result) {
	l.host.OnComplete(ctx, p, r)
	l.completed.Add(1)
}

type leakEnv struct {
	ctx       context.Context
	host      *Host
	manager   *sysprocess.Manager
	closer    *frameCloser
	slots     *process.ChildSlots
	resolvers *ctxapi.FrameResolvers
	scopes    sync.Map
	started   atomic.Int64
	completed atomic.Int64
}

func newLeakEnv(t *testing.T) *leakEnv {
	t.Helper()
	env := &leakEnv{closer: &frameCloser{}}
	resolvers := ctxapi.NewFrameResolvers()
	env.resolvers = resolvers
	require.NoError(t, resolvers.Register("child_slots", 0, process.ChildSlotResolver))
	appCtx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	appCtx = ctxapi.WithFrameResolvers(appCtx, resolvers)

	lifecycle := &leakLifecycle{completed: &env.completed}
	sched := actor.NewScheduler(nil, actor.WithLifecycle(lifecycle))
	node := &leakNode{hosts: map[pid.HostID]relay.Receiver{}}
	env.host = NewHost(registry.NewID("test", "host"), &hostapi.EntryConfig{}, sched,
		&mockFactory{proc: &mockProcess{}}, newTestPIDGen(), zap.NewNop())
	lifecycle.host = env.host
	env.manager = sysprocess.NewManager(node, zap.NewNop())
	require.NoError(t, node.RegisterHost("test:host", env.host))
	env.ctx = process.WithManager(appCtx, env.manager)
	_, err := env.host.Start(env.ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = env.host.Stop(context.Background()) })
	return env
}

func (e *leakEnv) admission(p *leakProcess) *process.Admission {
	return &process.Admission{
		Factory: func() (process.Process, error) { return p, nil },
		Meta:    process.Meta{Method: "main"},
	}
}

// spawnChild starts an owned child from ctx, with a frame value counted by
// the env's closer.
func (e *leakEnv) spawnChild(ctx context.Context, parent pid.PID, mode childMode, slots bool) error {
	options := attrs.NewBag()
	options.Set(process.ProcessOwnedKey, true)
	if slots {
		options.Set(process.ProcessParentKey, parent)
	}
	e.started.Add(1)
	_, err := e.manager.Start(ctx, &process.Start{
		HostID:    "test:host",
		Source:    registry.NewID("eval.program", "child"),
		Options:   options,
		Context:   []ctxapi.Pair{{Key: leakCloserKey, Value: e.closer}},
		Admission: e.admission(&leakProcess{mode: mode}),
	})
	if err != nil {
		e.started.Add(-1)
	}
	return err
}

func (e *leakEnv) runOwner(t *testing.T, children []childMode, ownerMode childMode) {
	t.Helper()
	var scope *process.ExecutionScope
	owner := &leakProcess{mode: ownerMode}
	owner.init = func(ctx context.Context) error {
		scope = process.GetExecutionScope(ctx)
		e.scopes.Store(scope, struct{}{})
		parent, _ := apiruntime.GetFramePID(ctx)
		for _, mode := range children {
			if err := e.spawnChild(ctx, parent, mode, e.slots != nil); err != nil {
				return err
			}
		}
		return nil
	}
	pairs := []ctxapi.Pair{{Key: leakCloserKey, Value: e.closer}}
	if e.slots != nil {
		pairs = append(pairs, process.ChildSlotsPair(e.slots))
	}
	e.started.Add(1)
	_, err := e.host.Run(e.ctx, &process.Start{
		HostID:    "test:host",
		Source:    registry.NewID("eval.program", "owner"),
		Context:   pairs,
		Admission: e.admission(owner),
	})
	require.NoError(t, err)
}

func settle(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("not settled: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func heapInUse() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestOwnedProcessCyclesLeaveNothingBehind(t *testing.T) {
	env := newLeakEnv(t)
	env.slots = process.NewChildSlots(1 << 20)

	scenarios := []struct {
		name     string
		children []childMode
		owner    childMode
	}{
		{"children complete", []childMode{childDone, childDone}, childDone},
		{"children fail", []childMode{childFail, childFail}, childDone},
		{"owner ends first", []childMode{childBlock, childBlock}, childDone},
		{"owner fails with children", []childMode{childBlock, childDone}, childFail},
	}
	batch := func(n int) {
		for i := 0; i < n; i++ {
			for _, sc := range scenarios {
				env.runOwner(t, sc.children, sc.owner)
			}
		}
		settle(t, func() bool { return env.completed.Load() == env.started.Load() }, "processes complete")
		settle(t, func() bool { return env.closer.closed.Load() == env.started.Load() }, "frames released")
		settle(t, func() bool { return env.slots.InUse() == 0 }, "child slots released")
		env.scopes.Range(func(k, _ any) bool {
			require.Zero(t, k.(*process.ExecutionScope).Owned(), "owned registrations released")
			env.scopes.Delete(k)
			return true
		})
	}

	batch(50)
	goroutines := runtime.NumGoroutine()
	heap := heapInUse()
	for i := 0; i < 4; i++ {
		batch(500)
	}
	settle(t, func() bool { return runtime.NumGoroutine() <= goroutines+2 }, "goroutines stable")
	grown := int64(heapInUse()) - int64(heap)
	require.Less(t, grown, int64(4<<20), "heap grew by %d bytes over 8000 owner cycles", grown)
}

func TestOwnedSpawnFailuresReleaseEveryReservation(t *testing.T) {
	env := newLeakEnv(t)
	env.slots = process.NewChildSlots(1 << 20)
	probed := &probes{}
	require.NoError(t, env.resolvers.Register("probe", 1, probed.resolve))

	failures := map[string]func(*leakProcess) *process.Admission{
		"factory error": func(*leakProcess) *process.Admission {
			return &process.Admission{Factory: func() (process.Process, error) { return nil, errors.New("factory") }}
		},
		"init error": func(p *leakProcess) *process.Admission {
			p.init = func(context.Context) error { return errors.New("init") }
			return env.admission(p)
		},
		"worker class mismatch": func(p *leakProcess) *process.Admission {
			a := env.admission(p)
			a.Meta.WorkerClass = "not-a-class"
			return a
		},
		"lifecycle start error": func(p *leakProcess) *process.Admission {
			p.startErr = errors.New("start")
			return env.admission(p)
		},
	}
	for name, build := range failures {
		t.Run(name, func(t *testing.T) {
			var scope *process.ExecutionScope
			var failed atomic.Int64
			owner := &leakProcess{mode: childDone}
			owner.init = func(ctx context.Context) error {
				scope = process.GetExecutionScope(ctx)
				parent, _ := apiruntime.GetFramePID(ctx)
				options := attrs.NewBag()
				options.Set(process.ProcessOwnedKey, true)
				options.Set(process.ProcessParentKey, parent)
				for i := 0; i < 200; i++ {
					_, err := env.manager.Start(ctx, &process.Start{
						HostID:    "test:host",
						Source:    registry.NewID("eval.program", "child"),
						Options:   options,
						Admission: build(&leakProcess{mode: childDone}),
					})
					if err != nil {
						failed.Add(1)
					}
				}
				return nil
			}
			env.started.Add(1)
			_, err := env.host.Run(env.ctx, &process.Start{
				HostID:    "test:host",
				Source:    registry.NewID("eval.program", "owner"),
				Context:   []ctxapi.Pair{process.ChildSlotsPair(env.slots)},
				Admission: env.admission(owner),
			})
			require.NoError(t, err)
			settle(t, func() bool { return env.completed.Load() >= 1 }, "owner completes")
			require.EqualValues(t, 200, failed.Load())
			require.Zero(t, scope.Owned(), "failed spawns leave no registration")
			require.Zero(t, env.slots.InUse(), "failed spawns leave no slot")
			settle(t, func() bool { return probed.unreleased() == 0 }, "attachments released")
			env.completed.Store(0)
			env.started.Store(0)
		})
	}
}

// An owner that fails to start ends its execution: the children it already
// spawned end with it.
func TestFailedOwnerStartTerminatesItsChildren(t *testing.T) {
	cases := map[string]func(*leakProcess){
		"init error":            func(p *leakProcess) { p.afterInit = errors.New("init") },
		"lifecycle start error": func(p *leakProcess) { p.startErr = errors.New("start") },
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			env := newLeakEnv(t)
			var scope *process.ExecutionScope
			owner := &leakProcess{mode: childDone}
			fail(owner)
			owner.init = func(ctx context.Context) error {
				scope = process.GetExecutionScope(ctx)
				parent, _ := apiruntime.GetFramePID(ctx)
				if err := env.spawnChild(ctx, parent, childBlock, false); err != nil {
					return err
				}
				return owner.afterInit
			}
			_, err := env.host.Run(env.ctx, &process.Start{
				HostID:    "test:host",
				Source:    registry.NewID("eval.program", "owner"),
				Admission: env.admission(owner),
			})
			require.Error(t, err)
			settle(t, func() bool { return env.completed.Load() == 1 }, "the owned child is terminated")
			require.Zero(t, scope.Owned())
		})
	}
}

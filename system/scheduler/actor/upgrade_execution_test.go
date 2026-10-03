// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

var (
	testExecutionKey = &ctxapi.Key{Name: "test.execution", Execution: true}
	// testPortKey holds an execution value that owns a resource, as a
	// terminal port does.
	testPortKey  = &ctxapi.Key{Name: "test.port", Execution: true}
	testLocalKey = &ctxapi.Key{Name: "test.local"}
)

// countingCloser records how often the frame holding it is reclaimed.
type countingCloser struct {
	first  sync.Once
	closed atomic.Int32
	// reclaimed is closed by the first Close.
	reclaimed chan struct{}
}

func newCountingCloser() *countingCloser {
	return &countingCloser{reclaimed: make(chan struct{})}
}

func (c *countingCloser) Close() error {
	c.closed.Add(1)
	c.first.Do(func() { close(c.reclaimed) })
	return nil
}

// incarnation records what each code incarnation sees of its frame and
// upgrades into the next one until none is left.
type incarnation struct {
	seen atomic.Pointer[incarnationView]
	next *process.UpgradeRequest
}

type incarnationView struct {
	execution any
	pid       pidapi.PID
	source    registry.ID
	hasPID    bool
}

func (p *incarnation) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	view := &incarnationView{}
	view.source, _ = runtime.GetFrameID(ctx)
	view.pid, view.hasPID = runtime.GetFramePID(ctx)
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		view.execution, _ = fc.Get(testExecutionKey)
	}
	p.seen.Store(view)
	return nil
}

func (p *incarnation) Step(_ []process.Event, out *process.StepOutput) error {
	if p.next != nil {
		out.SetUpgrade(p.next)
		return nil
	}
	out.Done(nil)
	return nil
}

func (*incarnation) Send(*relay.Package) error { return nil }
func (*incarnation) Close()                    {}

func TestUpgradeContinuesTheExecution(t *testing.T) {
	// completed reports whether completion ran on the execution's frame.
	completed := make(chan bool, 1)
	sched := newTestSchedulerWithLifecycle(1, &testLifecycle{
		onComplete: func(ctx context.Context, _ pidapi.PID, _ *runtime.Result) {
			fc := ctxapi.FrameFromContext(ctx)
			onExecutionFrame := fc != nil && fc.Has(testLocalKey)
			// The host completes and releases the frame it created.
			ctxapi.CompleteFrame(ctx)
			if fc != nil {
				_ = fc.Close()
			}
			completed <- onExecutionFrame
		},
	})
	sched.Start()
	defer testStopScheduler(sched)

	third := &incarnation{}
	second := &incarnation{next: &process.UpgradeRequest{Source: registry.NewID("app", "third")}}
	first := &incarnation{next: &process.UpgradeRequest{Source: registry.NewID("app", "second")}}
	appCtx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	process.WithFactory(appCtx, &mockFactory{
		createFunc: func(id registry.ID) (process.Process, *process.Meta, error) {
			if id.Name == "second" {
				return second, &process.Meta{}, nil
			}
			return third, &process.Meta{}, nil
		},
	})

	self := pidapi.PID{UniqID: "upgrader"}
	root := newCountingCloser()
	port := newCountingCloser()
	rootCtx, fc := ctxapi.OpenFrameContext(appCtx)
	if err := fc.SetMultiple(
		ctxapi.Pair{Key: runtime.FrameIDKey, Value: registry.NewID("app", "first")},
		ctxapi.Pair{Key: runtime.FramePIDKey, Value: self},
		ctxapi.Pair{Key: testExecutionKey, Value: "execution"},
		ctxapi.Pair{Key: testLocalKey, Value: root},
		ctxapi.Pair{Key: testPortKey, Value: port},
	); err != nil {
		t.Fatal(err)
	}
	fc.Seal()

	if _, err := sched.Submit(rootCtx, self, first, "", nil); err != nil {
		t.Fatal(err)
	}
	var onExecutionFrame bool
	select {
	case onExecutionFrame = <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not complete")
	}

	for name, inc := range map[string]*incarnation{"second": second, "third": third} {
		view := inc.seen.Load()
		if view == nil {
			t.Fatalf("%s incarnation was not initialized", name)
		}
		if view.source.Name != name {
			t.Errorf("%s incarnation runs as %v", name, view.source)
		}
		if !view.hasPID || view.pid != self {
			t.Errorf("%s incarnation keeps the process PID, got %v", name, view.pid)
		}
		if view.execution != "execution" {
			t.Errorf("%s incarnation sees the execution's values, got %v", name, view.execution)
		}
	}
	if !onExecutionFrame {
		t.Error("completion is reported on the execution's frame, not an incarnation's")
	}
	for _, c := range []*countingCloser{root, port} {
		select {
		case <-c.reclaimed:
		case <-time.After(5 * time.Second):
			t.Fatal("the execution frame was not reclaimed")
		}
	}
	if got := root.closed.Load(); got != 1 {
		t.Fatalf("the execution frame is reclaimed once after completion, got %d", got)
	}
	if got := port.closed.Load(); got != 1 {
		t.Fatalf("an execution resource is closed once, by the execution's frame, got %d", got)
	}
}

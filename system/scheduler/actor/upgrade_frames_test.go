// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
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

var upgradeFrameKey = &ctxapi.Key{Name: "test.incarnation.frame"}

// frameProbe is held by the frame of one code incarnation.
type frameProbe struct{ closed *atomic.Int64 }

func (p frameProbe) Close() error {
	p.closed.Add(1)
	return nil
}

// chainIncarnation holds a probe in its own frame and upgrades until
// remaining runs out.
type chainIncarnation struct {
	closed    *atomic.Int64
	remaining *atomic.Int64
	// initial runs in the sealed execution frame, which holds no probe.
	initial bool
}

func (p *chainIncarnation) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	if p.initial {
		return nil
	}
	return ctxapi.FrameFromContext(ctx).Set(upgradeFrameKey, frameProbe{closed: p.closed})
}

func (p *chainIncarnation) Step(_ []process.Event, out *process.StepOutput) error {
	if p.remaining.Add(-1) >= 0 {
		out.SetUpgrade(&process.UpgradeRequest{Source: registry.NewID("app", "next")})
		return nil
	}
	out.Done(nil)
	return nil
}

func (*chainIncarnation) Send(*relay.Package) error { return nil }
func (*chainIncarnation) Close()                    {}

func TestUpgradeChainReleasesEveryIncarnationFrame(t *testing.T) {
	const upgrades = 300
	var incarnationFrames atomic.Int64
	remaining := &atomic.Int64{}
	remaining.Store(upgrades)

	done := make(chan struct{})
	sched := newTestSchedulerWithLifecycle(1, &testLifecycle{
		onComplete: func(ctx context.Context, _ pidapi.PID, _ *runtime.Result) {
			ctxapi.CompleteFrame(ctx)
			if fc := ctxapi.FrameFromContext(ctx); fc != nil {
				_ = fc.Close()
			}
			close(done)
		},
	})
	sched.Start()
	defer testStopScheduler(sched)

	appCtx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	process.WithFactory(appCtx, &mockFactory{
		createFunc: func(registry.ID) (process.Process, *process.Meta, error) {
			return &chainIncarnation{closed: &incarnationFrames, remaining: remaining}, &process.Meta{}, nil
		},
	})

	self := pidapi.PID{UniqID: "chain"}
	rootCtx, fc := ctxapi.OpenFrameContext(appCtx)
	if err := fc.SetMultiple(
		ctxapi.Pair{Key: runtime.FrameIDKey, Value: registry.NewID("app", "first")},
		ctxapi.Pair{Key: runtime.FramePIDKey, Value: self},
	); err != nil {
		t.Fatal(err)
	}
	fc.Seal()

	if _, err := sched.Submit(rootCtx, self, &chainIncarnation{closed: &incarnationFrames, remaining: remaining, initial: true}, "", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("process did not complete")
	}
	deadline := time.Now().Add(5 * time.Second)
	for incarnationFrames.Load() < upgrades && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := incarnationFrames.Load(); got != upgrades {
		t.Fatalf("each upgraded incarnation's frame is released once: want %d, got %d", upgrades, got)
	}
}

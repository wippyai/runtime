// SPDX-License-Identifier: MPL-2.0
package kv

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/relay"
)

type cancelReadRouter struct {
	started chan struct{}
	blocked bool
	calls   atomic.Int32
}

func (r *cancelReadRouter) Send(*relay.Package) error { panic("context-aware sender was bypassed") }
func (r *cancelReadRouter) SendContext(ctx context.Context, pkg *relay.Package) error {
	r.calls.Add(1)
	select {
	case r.started <- struct{}{}:
	default:
	}
	if r.blocked {
		<-ctx.Done()
		return ctx.Err()
	}
	relay.ReleasePackage(pkg)
	return nil // accepted request, but no reply arrives
}

func TestForwardReadCallerCancellation(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		name := "waiting-reply"
		if blocked {
			name = "blocked-send"
		}
		t.Run(name, func(t *testing.T) {
			fsm := NewRaftFSM(nil)
			router := &cancelReadRouter{started: make(chan struct{}, 1), blocked: blocked}
			engine := NewRaftEngine(&fakeRaft{fsm: fsm, leaderID: "leader"}, fsm, nil, "client", router, nil)
			if err := engine.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = engine.Stop() })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := engine.GetViaLeaderContext(ctx, "claim"); done <- err }()
			select {
			case <-router.started:
			case <-time.After(time.Second):
				t.Fatal("read did not enter sender")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("read error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("read ignored caller cancellation")
			}
			engine.fwdMu.Lock()
			pending := len(engine.pendingReads)
			engine.fwdMu.Unlock()
			if pending != 0 {
				t.Fatalf("canceled correlation retained: %d", pending)
			}
			if router.calls.Load() != 1 {
				t.Fatalf("canceled read retried: %d", router.calls.Load())
			}
			if err := engine.ctx.Err(); err != nil {
				t.Fatalf("caller cancellation stopped shared engine: %v", err)
			}
		})
	}
}

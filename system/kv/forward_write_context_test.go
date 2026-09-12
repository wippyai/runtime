// SPDX-License-Identifier: MPL-2.0
package kv

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type cancelWriteRouter struct {
	leader  *fakeRaft
	started chan struct{}
	blocked bool
	calls   atomic.Int32
}

func (r *cancelWriteRouter) Send(*relay.Package) error { panic("write context sender bypassed") }
func (r *cancelWriteRouter) SendContext(ctx context.Context, pkg *relay.Package) error {
	r.calls.Add(1)
	if r.blocked {
		close(r.started)
		<-ctx.Done()
		return ctx.Err()
	}
	env := pkg.Messages[0].Payloads[0].Data().([]byte)
	_, err := r.leader.Apply(env[9:], time.Second)
	if err != nil {
		return err
	}
	relay.ReleasePackage(pkg)
	close(r.started)
	return nil // committed, but the response is lost
}

func TestForwardTxnCancellationDoesNotReplayCommittedWrite(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		name := "committed-reply-lost"
		if blocked {
			name = "blocked-send"
		}
		t.Run(name, func(t *testing.T) {
			fsm := NewRaftFSM(nil)
			router := &cancelWriteRouter{leader: &fakeRaft{fsm: fsm, leader: true}, started: make(chan struct{}), blocked: blocked}
			engine := NewRaftEngine(&fakeRaft{fsm: fsm, leaderID: "leader"}, fsm, nil, "client", router, nil)
			if err := engine.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = engine.Stop() })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := engine.TxnContext(ctx, []kvapi.TxnOp{{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: "claim", Value: []byte("committed")}})
				done <- err
			}()
			select {
			case <-router.started:
			case <-time.After(time.Second):
				t.Fatal("transaction did not reach sender")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("transaction error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("transaction ignored cancellation")
			}
			if router.calls.Load() != 1 {
				t.Fatalf("ambiguous mutation replayed: %d", router.calls.Load())
			}
			engine.fwdMu.Lock()
			pending := len(engine.pending)
			engine.fwdMu.Unlock()
			if pending != 0 {
				t.Fatalf("pending write correlation leaked: %d", pending)
			}
			entry, err := engine.Get("claim")
			if blocked {
				if !errors.Is(err, kvapi.ErrKeyNotFound) {
					t.Fatalf("blocked send mutated state: %v", err)
				}
			} else if err != nil || string(entry.Value) != "committed" {
				t.Fatalf("cancellation must not imply rollback of committed mutation: %+v %v", entry, err)
			}
			if engine.ctx.Err() != nil {
				t.Fatal("caller cancellation stopped engine")
			}
		})
	}
}

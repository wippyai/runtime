// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

type rejectingLeaderRead struct{ calls atomic.Int32 }

func (r *rejectingLeaderRead) GetViaLeader(string) (kvapi.Entry, error) {
	r.calls.Add(1)
	return kvapi.Entry{}, errors.New("successful Strong registration must not read via leader")
}

// promoteBeforeTxnReply models a committed pending write observed and promoted
// before the submitting caller receives its transaction response.
type promoteBeforeTxnReply struct {
	kvapi.Engine
	active chan struct{}
	once   sync.Once
}

func (e *promoteBeforeTxnReply) Txn(ops []kvapi.TxnOp) (bool, error) {
	committed, err := e.Engine.Txn(ops)
	if !committed || err != nil {
		return committed, err
	}
	for _, op := range ops {
		if op.Kind != kvapi.TxnPut {
			continue
		}
		if op.Key == activeKey("claim") {
			e.once.Do(func() { close(e.active) })
		}
		if op.Key == pendingKey("claim") {
			// Give the real KV watcher and Strong reconciler time to promote the
			// committed pending before the writer receives its transaction reply.
			select {
			case <-e.active:
			case <-time.After(2 * time.Second):
				return false, context.DeadlineExceeded
			}
		}
	}
	return true, nil
}

func TestStrongObserverMayPromoteBeforePendingTransactionReply(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatalf("start Strong watcher: %v", err)
	}
	base := r.engine
	r.engine = &promoteBeforeTxnReply{
		Engine: base,
		active: make(chan struct{}),
	}
	owner := mkPID("node-1", "owner")
	out, err := r.RegisterScope(t.Context(), "claim", owner, globalapi.Strong)
	if err != nil || out.State != globalapi.RegisterStateActive || !out.PID.Equal(owner) {
		t.Fatalf("already-promoted attempt returned %+v, %v; want active owner", out, err)
	}
}

func TestStrongSuccessfulRegisterUsesCommittedWatchWithoutLeaderRead(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	probe := &rejectingLeaderRead{}
	r.leaderRead = probe
	owner := mkPID("node-1", "local-owner")
	out, err := r.RegisterScope(t.Context(), "watch-success", owner, globalapi.Strong)
	if err != nil || out.State != globalapi.RegisterStateActive {
		t.Fatalf("watch-based registration: %+v, %v", out, err)
	}
	if reads := probe.calls.Load(); reads != 0 {
		t.Fatalf("success required %d leader reads", reads)
	}
}

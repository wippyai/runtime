// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

type beforeExpiryEngine struct {
	kvapi.Engine
	before func()
}

func (e *beforeExpiryEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	reader, ok := e.Engine.(kvapi.LocalSnapshotReader)
	if !ok {
		return nil, 0, kvapi.ErrKVClosed
	}
	return reader.ReadLocalSnapshot(keys)
}

func (e *beforeExpiryEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	expiry := false
	for _, op := range ops {
		if op.Kind == kvapi.TxnPut && op.Key == activeKey("claim") {
			return e.Engine.Txn(ops)
		}
		if op.Kind == kvapi.TxnDelete && op.Key == pendingKey("claim") {
			expiry = true
		}
	}
	if expiry && e.before != nil {
		before := e.before
		e.before = nil
		before()
	}
	return e.Engine.Txn(ops)
}

func TestExpiryValidatesVotesAtCommit(t *testing.T) {
	for _, reject := range []bool{false, true} {
		name := "ack"
		if reject {
			name = "reject"
		}
		t.Run(name, func(t *testing.T) {
			r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
			owner := mkPID("node-1", "owner")
			hdr := pendingHeader{PID: owner.String(), Name: "claim", AttemptID: "attempt-expiry", RequiredNodes: []pid.NodeID{"node-1", "peer"}, DeadlineUnixNano: time.Now().Add(-time.Second).UnixNano()}
			value, err := encode(hdr)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.engine.Set(pendingKey("claim"), value); err != nil {
				t.Fatal(err)
			}
			pe, err := r.engine.Get(pendingKey("claim"))
			if err != nil {
				t.Fatal(err)
			}
			epoch := pe.Epoch
			t.Cleanup(func() { r.strong.stopTimer("claim", hdr.AttemptID) })
			r.strong.addWaiter("claim", &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1), attemptID: hdr.AttemptID, pid: owner})
			if _, err := r.engine.Set(ackKey("claim", "attempt-expiry", "node-1"), []byte("node-1")); err != nil {
				t.Fatal(err)
			}
			base := r.engine
			r.engine = &beforeExpiryEngine{Engine: base, before: func() {
				key := ackKey("claim", "attempt-expiry", "peer")
				if reject {
					key = rejectKey("claim", "attempt-expiry", "peer")
				}
				if _, err := base.Set(key, []byte("peer")); err != nil {
					t.Fatal(err)
				}
			}}
			r.strong.leaderExpire("claim", epoch, pe.Version, hdr, "deadline")
			if _, err := base.Get(pendingKey("claim")); err != nil {
				t.Fatalf("stale timeout decision removed claim: %v", err)
			}

			r.strong.reconcile("claim")
			if reject {
				reason, _, _ := r.strong.takeTerminal("attempt-expiry")
				if reason != strongRejectConflict {
					t.Fatalf("committed rejection became timeout: %q", reason)
				}
			} else if _, err := base.Get(activeKey("claim")); err != nil {
				t.Fatalf("committed ACK set did not promote: %v", err)
			}
		})
	}
}

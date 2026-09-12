// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type beforeExpiryEngine struct {
	kvapi.Engine
	before func()
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
			t.Cleanup(func() { r.strong.stopTimer("claim") })
			owner := mkPID("node-1", "owner")
			hdr := pendingHeader{PID: owner.String(), Name: "claim", RequiredNodes: []pid.NodeID{"node-1", "peer"}, DeadlineUnixNano: time.Now().Add(-time.Second).UnixNano()}
			hdr.AttemptID = "expiry-race"
			if committed, err := r.strong.createPending(context.Background(), hdr); err != nil || !committed {
				t.Fatalf("admission failed: %v", err)
			}
			pe, err := r.engine.Get(pendingKey("claim"))
			if err != nil {
				t.Fatal(err)
			}
			epoch := pe.Epoch
			if _, err := r.engine.Set(ackKey("claim", epoch, "node-1"), []byte("node-1")); err != nil {
				t.Fatal(err)
			}
			base := r.engine
			r.engine = &beforeExpiryEngine{Engine: base, before: func() {
				key := ackKey("claim", epoch, "peer")
				if reject {
					key = rejectKey("claim", epoch, "peer")
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
				_, result, err := readStrongOutcome(base.Get, hdr.AttemptID, "claim", owner.String(), r.strong.resultPolicy.RecordBytes)
				if err != nil {
					t.Fatal(err)
				}
				reason := result.Reason
				if reason != strongRejectConflict {
					t.Fatalf("committed rejection became timeout: %q", reason)
				}
			} else if _, err := base.Get(activeKey("claim")); err != nil {
				t.Fatalf("committed ACK set did not promote: %v", err)
			}
		})
	}
}

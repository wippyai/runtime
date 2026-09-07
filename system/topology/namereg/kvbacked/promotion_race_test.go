// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type beforePromotionEngine struct {
	kvapi.Engine
	before func()
}

func (e *beforePromotionEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	for _, op := range ops {
		if op.Kind == kvapi.TxnPut && op.Key == activeKey("claim") && e.before != nil {
			before := e.before
			e.before = nil
			before()
			break
		}
	}
	return e.Engine.Txn(ops)
}

func TestStrongRejectCommittedBeforePromotionWins(t *testing.T) {
	testStrongChangedAdmission(t, true)
}

func TestStrongMissingAckPreventsPromotion(t *testing.T) {
	testStrongChangedAdmission(t, false)
}

func testStrongChangedAdmission(t *testing.T, reject bool) {
	t.Helper()
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	p := mkPID("node-1", "owner")
	hdr := pendingHeader{PID: p.String(), Name: "claim", RequiredNodes: []pid.NodeID{"node-1"}}
	encoded, err := encode(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey("claim"), encoded); err != nil {
		t.Fatal(err)
	}
	pe, err := r.engine.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(ackKey("claim", pe.Epoch, "node-1"), []byte("node-1")); err != nil {
		t.Fatal(err)
	}
	base := r.engine
	r.engine = &beforePromotionEngine{Engine: base, before: func() {
		// A rejection wins the Raft order after the leader's ACK scan but
		// before its promotion transaction. Header version remains unchanged.
		if reject {
			if _, err := base.Set(rejectKey("claim", pe.Epoch, "node-1"), []byte(strongRejectConflict)); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := base.Delete(ackKey("claim", pe.Epoch, "node-1")); err != nil {
				t.Fatal(err)
			}
		}
	}}
	r.strong.leaderPromote("claim", pe.Epoch, pe.Version, hdr)
	got, err := r.Lookup(context.Background(), "claim")
	if err != nil || got.Found {
		t.Fatalf("rejected claim promoted: %+v err=%v", got, err)
	}
}

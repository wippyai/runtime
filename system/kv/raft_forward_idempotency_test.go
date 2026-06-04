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

// dropFirstReplyRouter delivers forward requests to their target (so the leader
// commits them) but swallows the FIRST forward response, modeling a reply lost
// or delayed past the follower's wait. Later traffic flows normally.
type dropFirstReplyRouter struct {
	engines map[string]*RaftEngine
	dropped atomic.Bool
}

func (r *dropFirstReplyRouter) Send(pkg *relay.Package) error {
	for _, m := range pkg.Messages {
		if m.Topic == topicKVForwardResp && r.dropped.CompareAndSwap(false, true) {
			relay.ReleasePackage(pkg)
			return nil
		}
	}
	e, ok := r.engines[pkg.Target.Node]
	if !ok {
		relay.ReleasePackage(pkg)
		return errors.New("no engine for target")
	}
	return e.Send(pkg)
}

// TestRaftEngine_ForwardTimeoutDoesNotDoubleApply is the B3 regression: when a
// forwarded write commits on the leader but its reply is lost, the follower must
// time out with errForwardTimeout and NOT retry. A retry re-applies the
// non-idempotent op; the leader FSM must show exactly one apply (version 1). The
// pre-fix code mistook the timeout for a not-leader rejection and retried,
// yielding version 2.
func TestRaftEngine_ForwardTimeoutDoesNotDoubleApply(t *testing.T) {
	router := &dropFirstReplyRouter{}
	aFSM := NewRaftFSM(nil)
	bFSM := NewRaftFSM(nil)
	a := NewRaftEngine(&fakeRaft{fsm: aFSM, leader: true, leaderID: "A"}, aFSM, nil, "A", router, nil)
	b := NewRaftEngine(&fakeRaft{fsm: bFSM, leader: false, leaderID: "A"}, bFSM, nil, "B", router, nil)
	b.forwardWait = 150 * time.Millisecond
	router.engines = map[string]*RaftEngine{"A": a, "B": b}
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(); _ = b.Stop() })

	_, err := b.Set("k", []byte("v"))
	if !errors.Is(err, errForwardTimeout) {
		t.Fatalf("forwarded set with dropped reply = %v, want errForwardTimeout", err)
	}
	e, ok := aFSM.get("k")
	if !ok {
		t.Fatalf("leader FSM should have applied the forwarded write once")
	}
	if e.Version != 1 {
		t.Fatalf("forwarded write applied to FSM version %d, want exactly 1 (no double-apply)", e.Version)
	}
}

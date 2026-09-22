// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type directedVoteEngine struct {
	kvapi.Engine
	err   error
	scans int
}

func (e *directedVoteEngine) Get(key string) (kvapi.Entry, error) {
	if e.err != nil {
		return kvapi.Entry{}, e.err
	}
	return e.Engine.Get(key)
}
func (e *directedVoteEngine) Scan(prefix string, fn func(kvapi.Entry) bool) error {
	e.scans++
	return e.Engine.Scan(prefix, fn)
}
func (e *directedVoteEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}

func TestVoteRoutingTargetsOnlyCurrentClaim(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Hour, nil)
	base := r.engine
	wrapped := &directedVoteEngine{Engine: base}
	r.engine = wrapped
	r.strong.isLeader = func() bool { return false }
	for _, name := range []string{"claim", "other"} {
		owner := mkPID("node-1", name)
		value, err := encode(pendingHeader{Name: name, PID: owner.String(), AttemptID: name, RequiredNodes: []pid.NodeID{"node-1", "peer"}, DeadlineUnixNano: time.Now().Add(time.Hour).UnixNano()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := base.Set(pendingKey(name), value); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.handleWatchEvent(kvapi.WatchEvent{Current: &kvapi.Entry{Key: ackKey("claim", "claim", "peer")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Get(ackKey("claim", "claim", "node-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Get(ackKey("other", "other", "node-1")); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("unrelated claim was driven: %v", err)
	}
	if wrapped.scans != 0 {
		t.Fatalf("vote caused %d scans", wrapped.scans)
	}
	if err := base.Delete(pendingKey("claim")); err != nil {
		t.Fatal(err)
	}
	if err := r.handleWatchEvent(kvapi.WatchEvent{Type: kvapi.WatchDelete, Previous: &kvapi.Entry{Key: ackKey("claim", "claim", "peer")}}); err != nil {
		t.Fatal(err)
	}
	if wrapped.scans != 0 {
		t.Fatal("late vote delete scanned pending records")
	}
	r.strong.isLeader = func() bool { return true }
	key := rejectKey("other", "other", "peer")
	if _, err := base.Set(key, []byte(strongRejectConflict)); err != nil {
		t.Fatal(err)
	}
	if err := r.handleWatchEvent(kvapi.WatchEvent{Current: &kvapi.Entry{Key: key}}); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Get(pendingKey("other")); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("NACK did not expire its claim: %v", err)
	}
	if wrapped.scans != 0 {
		t.Fatal("NACK scanned pending records")
	}
}

func TestVoteRoutingUsesCanonicalComponents(t *testing.T) {
	r := newStrongReg(t, nil, time.Hour, nil)
	owner := mkPID("node-1", "owner")
	for _, name := range []string{"", "plain", "名:%3A:%25"} {
		node := "peer:%3A:节点"
		value, err := encode(pendingHeader{Name: name, PID: owner.String(), AttemptID: "attempt", RequiredNodes: []pid.NodeID{node}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.engine.Set(pendingKey(name), value); err != nil {
			t.Fatal(err)
		}
		for _, prefix := range []string{ackPrefix, rejectPrefix} {
			key := prefix + voteComponent(name) + ":attempt:" + voteComponent(node)
			got, ok, err := r.strongVoteName(key, prefix)
			if err != nil || !ok || got != name {
				t.Fatalf("route %q: %q %v %v", key, got, ok, err)
			}
			for _, bad := range []string{key + ":extra", prefix + "%3a:attempt:peer", prefix + "%XX:attempt:peer", prefix + voteComponent(name) + ":stale:" + voteComponent(node), prefix + voteComponent(name) + ":attempt:unknown", prefix + ":", "wrong"} {
				if got, ok, err := r.strongVoteName(bad, prefix); err != nil || ok {
					t.Fatalf("invalid vote %q routed as %q %v %v", bad, got, ok, err)
				}
			}
		}
	}
}

func TestVoteRoutingErrorsCloseAdmission(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "record"}[corrupt], func(t *testing.T) {
			r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Hour, nil)
			base := r.engine
			putPendingAttempt(t, base, "attempt", mkPID("node-1", "owner"), []pid.NodeID{"node-1"})
			failure := errors.New("read unavailable")
			wrapped := &directedVoteEngine{Engine: base, err: failure}
			r.engine = wrapped
			if corrupt {
				wrapped.err = nil
				if _, err := base.Set(pendingKey("claim"), []byte{0xc1}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			run := &reconcilerLifecycle{ctx: ctx, cancel: cancel}
			r.reconciler.Store(run)
			r.ready.Store(true)
			err := r.handleWatchEvent(kvapi.WatchEvent{Current: &kvapi.Entry{Key: ackKey("claim", "attempt", "node-1")}})
			if err == nil || (!corrupt && !errors.Is(err, failure)) {
				t.Fatalf("failure swallowed: %v", err)
			}
			r.failReconciler(run, err)
			if r.ready.Load() || ctx.Err() == nil {
				t.Fatal("vote failure left admission open")
			}
		})
	}
}

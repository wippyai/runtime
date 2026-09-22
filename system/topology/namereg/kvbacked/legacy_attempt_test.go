// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

func legacyPending(t *testing.T, r *Service, deadline time.Time) kvapi.Entry {
	t.Helper()
	owner := mkPID("node-1", "owner")
	// The released payload has no attempt field.
	data, err := encode(map[string]any{
		"p": owner.String(), "n": "claim", "d": "node-1",
		"r": []string{"node-1", "peer"}, "dl": deadline.UnixNano(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey("claim"), data); err != nil {
		t.Fatal(err)
	}
	e, err := r.engine.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestLegacyPendingRestartPreservesRecordAndVotes(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
	before := legacyPending(t, r, time.Now().Add(time.Minute))
	key := ackKey("claim", before.Epoch, "node-1")
	if _, err := r.engine.Set(key, []byte("node-1")); err != nil {
		t.Fatal(err)
	}
	vote, err := r.engine.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	var identity string
	for range 3 {
		restarted := NewService(r.engine, "node-1", nil, nil)
		restarted.ConfigureStrong(StrongDeps{IsLeader: func() bool { return false }})
		ctx, cancel := context.WithCancel(t.Context())
		if err := restarted.StartReconciler(ctx); err != nil {
			cancel()
			t.Fatal(err)
		}
		if owner, held := restarted.IsStrongReserved("claim"); !held || !owner.Equal(mkPID("node-1", "owner")) {
			cancel()
			t.Fatalf("restart lost the acknowledged exclusion: owner=%v held=%v", owner, held)
		}
		restarted.strong.mu.Lock()
		got := restarted.strong.exclusions["claim"].attemptID
		restarted.strong.mu.Unlock()
		cancel()
		if got == "" || (identity != "" && got != identity) {
			t.Fatalf("unstable legacy identity: %q -> %q", identity, got)
		}
		identity = got
	}
	after, err := r.engine.Get(before.Key)
	if err != nil || after.Version != before.Version || after.Epoch != before.Epoch || !bytes.Equal(after.Value, before.Value) {
		t.Fatalf("startup rewrote legacy reservation: before=%+v after=%+v err=%v", before, after, err)
	}
	afterVote, err := r.engine.Get(key)
	if err != nil || afterVote.Version != vote.Version || !bytes.Equal(afterVote.Value, vote.Value) {
		t.Fatalf("startup changed vote: %+v %v", afterVote, err)
	}
}

func TestLegacyPendingCompletesWithExistingVotes(t *testing.T) {
	for _, outcome := range []string{"promote", "reject", "expire"} {
		t.Run(outcome, func(t *testing.T) {
			r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
			deadline := time.Now().Add(time.Minute)
			if outcome == "expire" {
				deadline = time.Now().Add(-time.Minute)
			}
			old := legacyPending(t, r, deadline)
			if _, err := r.engine.Set(ackKey("claim", old.Epoch, "node-1"), []byte("node-1")); err != nil {
				t.Fatal(err)
			}
			if outcome != "expire" {
				key := ackKey("claim", old.Epoch, "peer")
				if outcome == "reject" {
					key = rejectKey("claim", old.Epoch, "peer")
				}
				if _, err := r.engine.Set(key, []byte("peer")); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.strong.reconcile("claim"); err != nil {
				t.Fatal(err)
			}
			if _, err := r.engine.Get(old.Key); !errors.Is(err, kvapi.ErrKeyNotFound) {
				t.Fatalf("pending survived: %v", err)
			}
			active, err := r.engine.Get(activeKey("claim"))
			if outcome == "promote" {
				if err != nil {
					t.Fatal(err)
				}
				hdr, err := pendingFromEntry(old)
				if err != nil {
					t.Fatal(err)
				}
				av, err := decodeActive(active.Value)
				if err != nil || av.AttemptID != hdr.AttemptID || av.PID != hdr.PID {
					t.Fatalf("promotion changed owner/identity: %+v err=%v", av, err)
				}
				if err := r.handleWatchEvent(kvapi.WatchEvent{Type: kvapi.WatchDelete, Previous: &old}); err != nil {
					t.Fatal(err)
				}
				if _, held := r.IsStrongReserved("claim"); !held {
					t.Fatal("pending delete released promoted owner")
				}
			} else if !errors.Is(err, kvapi.ErrKeyNotFound) {
				t.Fatalf("failed claim became active: %+v err=%v", active, err)
			}
			for _, node := range []string{"node-1", "peer"} {
				for _, key := range []string{ackKey("claim", old.Epoch, node), rejectKey("claim", old.Epoch, node)} {
					if _, err := r.engine.Get(key); !errors.Is(err, kvapi.ErrKeyNotFound) {
						t.Fatalf("terminal vote retained: %s: %v", key, err)
					}
				}
			}
		})
	}
}

func TestLegacyAttemptIdentityUsesCommittedEntry(t *testing.T) {
	r := newStrongReg(t, nil, time.Second, nil)
	old := legacyPending(t, r, time.Now().Add(time.Minute))
	want, err := pendingFromEntry(old)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"version", "epoch", "name"} {
		e := old
		switch field {
		case "version":
			e.Version++
		case "epoch":
			e.Epoch++
		case "name":
			e.Key = pendingKey("other")
			hdr, err := decodePending(e.Value)
			if err != nil {
				t.Fatal(err)
			}
			hdr.Name = "other"
			e.Value, err = encode(hdr)
			if err != nil {
				t.Fatal(err)
			}
		}
		got, err := pendingFromEntry(e)
		if err != nil || got.AttemptID == want.AttemptID {
			t.Fatalf("%s reused identity: %+v err=%v", field, got, err)
		}
	}
	old.Version = 0
	if _, err := pendingFromEntry(old); err == nil {
		t.Fatal("unversioned legacy record accepted")
	}
}

func TestLegacyDelayedDeleteDoesNotCompleteReplacement(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	old := legacyPending(t, r, time.Now().Add(-time.Minute))
	if err := r.strong.reconcile("claim"); err != nil {
		t.Fatal(err)
	}
	owner := mkPID("node-1", "owner")
	out, err := r.RegisterScope(t.Context(), "claim", owner, globalapi.Strong)
	if err != nil || out.State != globalapi.RegisterStateActive {
		t.Fatalf("replacement failed: %+v err=%v", out, err)
	}
	active, err := r.engine.Get(activeKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	av, err := decodeActive(active.Value)
	if err != nil {
		t.Fatal(err)
	}
	waiter := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1), attemptID: av.AttemptID, pid: owner}
	r.strong.addWaiter("claim", waiter)
	defer r.strong.removeWaiter("claim", waiter)
	if err := r.handleWatchEvent(kvapi.WatchEvent{Type: kvapi.WatchDelete, Previous: &old}); err != nil {
		t.Fatal(err)
	}
	if got, held := r.IsStrongReserved("claim"); !held || !got.Equal(owner) {
		t.Fatalf("old delete released replacement: %v %v", got, held)
	}
	select {
	case result := <-waiter.ch:
		if result.State != globalapi.RegisterStateActive {
			t.Fatalf("old delete expired replacement: %+v", result)
		}
	default:
		t.Fatal("reconciliation did not observe the current active owner")
	}
}

func TestLegacyPendingMalformedStartupFailsClosed(t *testing.T) {
	r := newStrongReg(t, nil, time.Second, nil)
	r.strong.isLeader = func() bool { return false }
	old := legacyPending(t, r, time.Now().Add(time.Minute))
	hdr, err := decodePending(old.Value)
	if err != nil {
		t.Fatal(err)
	}
	hdr.Name = "wrong"
	value, err := encode(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(old.Key, value); err != nil {
		t.Fatal(err)
	}
	if err := r.StartReconciler(t.Context()); err == nil {
		t.Fatal("malformed legacy record admitted at startup")
	}
	if r.ready.Load() {
		t.Fatal("malformed legacy record opened readiness")
	}
}

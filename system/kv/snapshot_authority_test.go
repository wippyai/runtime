// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"errors"
	"testing"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type snapshotBarrierSubmitter struct {
	raftSubmitter
	barrierErr error
	calls      int
}

func (s *snapshotBarrierSubmitter) Barrier(time.Duration) error {
	s.calls++
	return s.barrierErr
}

// A populated local replica is not authority. Neither a failed barrier nor a
// non-voter forwarding target permits returning even a partial local snapshot.
func TestSnapshotAuthorityRejectsFailedBarrier(t *testing.T) {
	for _, cause := range []error{raftapi.ErrNotLeader, errors.New("quorum unavailable")} {
		t.Run(cause.Error(), func(t *testing.T) {
			eng, _ := newEngine(t)
			if _, err := eng.Set("claim", []byte("stale owner")); err != nil {
				t.Fatal(err)
			}
			submitter := &snapshotBarrierSubmitter{raftSubmitter: eng.raft, barrierErr: cause}
			eng.raft = submitter
			visited := false
			index, err := eng.ScanAtIndex("", func(kvapi.Entry) bool { visited = true; return true })
			if !errors.Is(err, cause) || index != 0 || visited || submitter.calls != 1 {
				t.Fatalf("failed barrier exposed state: index=%d visited=%v calls=%d err=%v", index, visited, submitter.calls, err)
			}
			// A later successful barrier may expose the coherent retained replica.
			submitter.barrierErr = nil
			index, err = eng.ScanAtIndex("", func(kvapi.Entry) bool { visited = true; return true })
			if err != nil || index == 0 || !visited || submitter.calls != 2 {
				t.Fatalf("recovered authority: index=%d visited=%v calls=%d err=%v", index, visited, submitter.calls, err)
			}
		})
	}
}

func TestSnapshotClientCannotTreatForwardTargetAsAuthority(t *testing.T) {
	eng, _ := newEngine(t)
	if _, err := eng.Set("claim", []byte("old replica")); err != nil {
		t.Fatal(err)
	}
	eng.raft = ClientSubmitter{Resolve: func() (raftapi.ServerID, bool) { return "reachable-member", true }}
	visited := false
	index, err := eng.ScanAtIndex("", func(kvapi.Entry) bool { visited = true; return true })
	if !errors.Is(err, raftapi.ErrNotLeader) || index != 0 || visited {
		t.Fatalf("client exposed replica: index=%d visited=%v err=%v", index, visited, err)
	}
}

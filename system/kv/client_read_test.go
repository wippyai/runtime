// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

// A registry non-member whose membership shows no raft member has nobody to
// read from: the forwarded read reports that at once instead of waiting for a
// member to appear.
func TestClientReadWithoutRaftMemberReportsNoLeaderOnce(t *testing.T) {
	var resolves atomic.Int32
	submitter := ClientSubmitter{Resolve: func() (raftapi.ServerID, bool) {
		resolves.Add(1)
		return "", false
	}}
	fsm := NewRaftFSM()
	eng := NewRaftEngine(submitter, fsm, "client", &truncatedReadRouter{}, nil)
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop() })

	_, err := eng.GetViaLeader("absent")
	if !errors.Is(err, errNoForwardLeader) {
		t.Fatalf("read without a raft member = %v, want errNoForwardLeader", err)
	}
	if got := resolves.Load(); got != 1 {
		t.Fatalf("read resolved the forward target %d times, want once", got)
	}
}

// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"errors"
	"testing"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

func TestClientSubmitter(t *testing.T) {
	t.Run("never leader, writes report not-leader so the engine forwards", func(t *testing.T) {
		c := ClientSubmitter{Resolve: func() (raftapi.ServerID, bool) { return "n1", true }}
		if c.IsLeader() {
			t.Fatal("client must never report leader")
		}
		if _, err := c.Apply(nil, time.Second); !errors.Is(err, raftapi.ErrNotLeader) {
			t.Fatalf("Apply err = %v, want ErrNotLeader", err)
		}
		if err := c.Barrier(time.Second); !errors.Is(err, raftapi.ErrNotLeader) {
			t.Fatalf("Barrier err = %v, want ErrNotLeader", err)
		}
		if c.CommitIndex() != 0 {
			t.Fatal("client CommitIndex must be 0")
		}
	})

	t.Run("Leader resolves the forward target", func(t *testing.T) {
		c := ClientSubmitter{Resolve: func() (raftapi.ServerID, bool) { return "member-7", true }}
		id, _, err := c.Leader()
		if err != nil || id != "member-7" {
			t.Fatalf("Leader = (%q, %v), want (member-7, nil)", id, err)
		}
	})

	t.Run("unresolved target reads as no-leader (engine retries)", func(t *testing.T) {
		for _, c := range []ClientSubmitter{
			{Resolve: nil},
			{Resolve: func() (raftapi.ServerID, bool) { return "", false }},
			{Resolve: func() (raftapi.ServerID, bool) { return "", true }},
		} {
			if _, _, err := c.Leader(); !errors.Is(err, raftapi.ErrNotLeader) {
				t.Fatalf("Leader err = %v, want ErrNotLeader", err)
			}
		}
	})
}

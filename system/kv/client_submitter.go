// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

// ClientSubmitter is the raft submitter a registry non-member (role=client)
// gives the RaftEngine: the node runs no raft Node, so every op is "not leader"
// and the engine forwards it over the relay to the member returned by Resolve.
// Resolve need not return the leader — that member re-forwards to the leader it
// can resolve (see maxForwardHops). Resolve returns ok=false when no eligible
// member is visible yet, which the engine treats as "no leader" and retries.
type ClientSubmitter struct {
	Resolve func() (raftapi.ServerID, bool)
}

func (c ClientSubmitter) Apply([]byte, time.Duration) (*raftapi.ApplyResponse, error) {
	return nil, raftapi.ErrNotLeader
}

func (c ClientSubmitter) IsLeader() bool { return false }

func (c ClientSubmitter) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	if c.Resolve == nil {
		return "", "", raftapi.ErrNotLeader
	}
	id, ok := c.Resolve()
	if !ok || id == "" {
		return "", "", raftapi.ErrNotLeader
	}
	return id, "", nil
}

func (c ClientSubmitter) Barrier(time.Duration) error { return raftapi.ErrNotLeader }

func (c ClientSubmitter) CommitIndex() uint64 { return 0 }

var _ raftSubmitter = ClientSubmitter{}

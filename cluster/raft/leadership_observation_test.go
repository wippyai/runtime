// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"testing"

	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"go.uber.org/zap"
)

func TestLeadershipObservationPreStartAndIndependentConsumers(t *testing.T) {
	n := NewNode("node", nil, raftapi.Config{}, nil, zap.NewNop(), nil, nil, nil)
	a := n.ObserveLeadership()
	b := n.ObserveLeadership()
	require.Equal(t, raftapi.Shutdown, a.State)
	require.Equal(t, a.Revision, b.Revision)
	n.setLeadership(raftapi.Follower, 1)
	select {
	case <-a.Changed:
	default:
		t.Fatal("subscriber created before Start missed Raft startup")
	}
	select {
	case <-b.Changed:
	default:
		t.Fatal("another subscriber consumed the startup notification")
	}
	current := n.ObserveLeadership()
	require.Equal(t, raftapi.Follower, current.State)
	require.Equal(t, uint64(1), current.Term)
	require.Greater(t, current.Revision, a.Revision)

	// A busy subscriber sees that the observed generation was invalidated,
	// even when the leadership state returns to its earlier value.
	n.setLeadership(raftapi.Leader, 2)
	n.setLeadership(raftapi.Follower, 3)
	n.setLeadership(raftapi.Leader, 4)
	select {
	case <-current.Changed:
	default:
		t.Fatal("intermediate leadership transitions were lost")
	}
	latest := n.ObserveLeadership()
	require.Equal(t, raftapi.Leader, latest.State)
	require.Equal(t, uint64(4), latest.Term)
	require.Equal(t, current.Revision+3, latest.Revision)
	n.setLeadership(raftapi.Leader, 4)
	require.Equal(t, latest.Revision, n.ObserveLeadership().Revision, "unchanged samples do not wake subscribers")
}

func TestLeadershipObservationStopCannotReopen(t *testing.T) {
	n := NewNode("node", nil, raftapi.Config{}, nil, zap.NewNop(), nil, nil, nil)
	n.setLeadership(raftapi.Leader, 2)
	before := n.ObserveLeadership()
	n.stopLeadership()
	<-before.Changed
	n.setLeadership(raftapi.Leader, 3) // an in-flight monitor sample after Stop
	after := n.ObserveLeadership()
	require.Equal(t, raftapi.Shutdown, after.State)
	require.Equal(t, before.Revision+1, after.Revision)
}

func TestLeadershipObservationLeaderIDCorrection(t *testing.T) {
	n := NewNode("node", nil, raftapi.Config{}, nil, zap.NewNop(), nil, nil, nil)
	n.setLeadership(raftapi.Follower, 2)
	before := n.ObserveLeadership()
	n.leadershipMu.Lock()
	n.updateLeadershipLocked(raftapi.Follower, 2, "leader")
	n.leadershipMu.Unlock()
	select {
	case <-before.Changed:
	default:
		t.Fatal("known-leader correction did not notify observers")
	}
	after := n.ObserveLeadership()
	require.Equal(t, "leader", after.LeaderID)
	require.Equal(t, before.Revision+1, after.Revision)
	n.leadershipMu.Lock()
	n.updateLeadershipLocked(raftapi.Follower, 2, "leader")
	n.leadershipMu.Unlock()
	require.Equal(t, after.Revision, n.ObserveLeadership().Revision)
}

// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"testing"

	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"go.uber.org/zap/zaptest"
)

// TestNodeIsMember_NotStarted verifies IsMember reads safely before Start
// wires up the underlying raft instance.
func TestNodeIsMember_NotStarted(t *testing.T) {
	n := NewNode("node-1", nil, raftapi.Config{}, nil, zaptest.NewLogger(t), nil, nil, nil)
	require.False(t, n.IsMember())
}

// TestNodeRole_NotStarted verifies Role returns "non-member" before Start.
func TestNodeRole_NotStarted(t *testing.T) {
	n := NewNode("node-1", nil, raftapi.Config{}, nil, zaptest.NewLogger(t), nil, nil, nil)
	require.Equal(t, "non-member", n.Role())
}

// TestNodeStats_NotStarted verifies Stats returns nil before Start.
func TestNodeStats_NotStarted(t *testing.T) {
	n := NewNode("node-1", nil, raftapi.Config{}, nil, zaptest.NewLogger(t), nil, nil, nil)
	require.Nil(t, n.Stats())
}

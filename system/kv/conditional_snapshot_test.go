// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

var _ kvapi.ConditionalSnapshotReader = (*Service)(nil)
var _ kvapi.ConditionalSnapshotReader = (*RaftEngine)(nil)

func TestConditionalSnapshotAlwaysConfirmsRaftAuthority(t *testing.T) {
	engine, _ := newEngine(t)
	_, err := engine.Set("claim", []byte("owner"))
	require.NoError(t, err)
	revision, err := engine.ScanAtIndex("", func(kvapi.Entry) bool { return true })
	require.NoError(t, err)
	require.NotZero(t, revision)
	submitter := &snapshotBarrierSubmitter{raftSubmitter: engine.raft, barrierErr: raftapi.ErrNotLeader}
	engine.raft = submitter
	visited := false
	current, unchanged, err := engine.ScanAtIndexSince("", revision, func(kvapi.Entry) bool { visited = true; return true })
	require.ErrorIs(t, err, raftapi.ErrNotLeader)
	require.Zero(t, current)
	require.False(t, unchanged)
	require.False(t, visited)
	require.Equal(t, 1, submitter.calls)
	submitter.barrierErr = nil
	current, unchanged, err = engine.ScanAtIndexSince("", revision, func(kvapi.Entry) bool { visited = true; return true })
	require.NoError(t, err)
	require.Equal(t, revision, current)
	require.True(t, unchanged)
	require.False(t, visited)
	require.Equal(t, 2, submitter.calls)
}

func TestConditionalSnapshotChangedAndZeroRevisionsScan(t *testing.T) {
	service := NewService("conditional", nil, nil)
	_, err := service.Start(context.Background())
	require.NoError(t, err)
	defer service.Stop(context.Background())
	_, err = service.Set("claim", []byte("first"))
	require.NoError(t, err)
	revision, err := service.ScanAtIndex("", func(kvapi.Entry) bool { return true })
	require.NoError(t, err)
	_, err = service.Set("claim", []byte("replacement"))
	require.NoError(t, err)
	for _, known := range []uint64{0, revision, revision + 100} {
		var entries []kvapi.Entry
		current, unchanged, err := service.ScanAtIndexSince("", known, func(entry kvapi.Entry) bool { entries = append(entries, entry); return true })
		require.NoError(t, err)
		require.False(t, unchanged)
		require.Greater(t, current, revision)
		require.Len(t, entries, 1)
		require.Equal(t, []byte("replacement"), entries[0].Value)
	}
}

func TestConditionalSnapshotForwardingClientCannotConfirmCache(t *testing.T) {
	engine, _ := newEngine(t)
	_, err := engine.Set("claim", []byte("stale"))
	require.NoError(t, err)
	_, revision, err := engine.ReadLocalSnapshot(nil)
	require.NoError(t, err)
	engine.raft = ClientSubmitter{Resolve: func() (raftapi.ServerID, bool) { return "member", true }}
	current, unchanged, err := engine.ScanAtIndexSince("", revision, func(kvapi.Entry) bool { t.Fatal("visited stale cache"); return false })
	require.True(t, errors.Is(err, raftapi.ErrNotLeader))
	require.Zero(t, current)
	require.False(t, unchanged)
}

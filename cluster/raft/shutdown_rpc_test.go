// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/cluster/internode"
	"go.uber.org/zap"
)

// Admission succeeds but the peer never answers: equivalent to an established
// connection whose remote process has stopped consuming Raft requests.
type unansweredRaftConn struct {
	*raftTransportConn
	sent chan struct{}
}

func (c *unansweredRaftConn) SendToNode(cluster.NodeID, []byte, internode.Class) error {
	select {
	case c.sent <- struct{}{}:
	default:
	}
	return nil
}

func TestNodeStopUnblocksOutstandingReplication(t *testing.T) {
	conn := &unansweredRaftConn{raftTransportConn: newRaftTransportFabric().conn("one"), sent: make(chan struct{}, 1)}
	transport, err := newRaftMessageTransport("one", conn, time.Minute, zap.NewNop())
	require.NoError(t, err)
	cfg := hraft.DefaultConfig()
	cfg.LocalID, cfg.LogOutput = "one", io.Discard
	cfg.HeartbeatTimeout, cfg.ElectionTimeout, cfg.LeaderLeaseTimeout = 50*time.Millisecond, 50*time.Millisecond, 50*time.Millisecond
	store := hraft.NewInmemStore()
	fsm := &shutdownTestFSM{}
	instance, err := hraft.NewRaft(cfg, fsm, store, store, hraft.NewInmemSnapshotStore(), transport)
	require.NoError(t, err)
	node := NewNode("one", fsm, raftapi.Config{ShutdownTransferTimeout: time.Millisecond}, nil, zap.NewNop(), nil, nil, nil)
	node.raft, node.transport, node.started = instance, transport, true
	t.Cleanup(func() { _ = transport.Close(); _ = instance.Shutdown().Error() })
	require.NoError(t, instance.BootstrapCluster(hraft.Configuration{Servers: []hraft.Server{{ID: "one", Address: "one", Suffrage: hraft.Voter}}}).Error())
	require.Eventually(t, func() bool { return instance.State() == hraft.Leader }, 5*time.Second, time.Millisecond)
	require.NoError(t, instance.AddNonvoter("unanswered", "unanswered", 0, time.Second).Error())
	select {
	case <-conn.sent:
	case <-time.After(5 * time.Second):
		t.Fatal("replication never started")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- node.Stop(context.Background()) }()
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(time.Second):
		// Release transport first so failure does not strand the shutdown worker.
		_ = transport.Close()
		require.NoError(t, <-stopped)
		t.Fatal("shutdown waited on the remote RPC deadline instead of closing transport")
	}
}

func TestTransportCloseUnblocksIncompleteSnapshot(t *testing.T) {
	conn := newRaftTransportFabric().conn("receiver")
	transport, err := newRaftMessageTransport("receiver", conn, time.Minute, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = transport.Close() })
	payload, err := encodeMsgpack(&hraft.InstallSnapshotRequest{Term: 3, Size: int64(raftSnapshotChunkSize)})
	require.NoError(t, err)
	wire, err := encodeMsgpack(&raftFrame{ID: 42, Type: raftRPCInstallSnapshot, Request: true, Payload: payload})
	require.NoError(t, err)
	transport.onFrame("sender", wire)
	rpc := <-transport.Consumer()
	read := make(chan error, 1)
	go func() { _, err := io.ReadAll(rpc.Reader); read <- err }()
	require.NoError(t, transport.Close())
	select {
	case err := <-read:
		require.ErrorIs(t, err, hraft.ErrTransportShutdown)
	case <-time.After(time.Second):
		t.Fatal("snapshot reader remained blocked after transport close")
	}
	transport.mu.Lock()
	require.Empty(t, transport.snapshots)
	transport.mu.Unlock()
	require.NoError(t, transport.Close(), "shutdown and Raft future may both close transport")
}

// This fixture exercises transport shutdown, with immediate FSM application.
type shutdownTestFSM struct{}

func (*shutdownTestFSM) Apply(*hraft.Log) any { return nil }
func (*shutdownTestFSM) Snapshot() (hraft.FSMSnapshot, error) {
	return nil, errors.New("snapshot unused in shutdown test")
}
func (*shutdownTestFSM) Restore(reader io.ReadCloser) error { return reader.Close() }

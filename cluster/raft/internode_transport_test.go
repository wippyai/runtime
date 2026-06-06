// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/cluster/internode"
	"go.uber.org/zap"
)

type raftTransportFabric struct {
	nodes map[cluster.NodeID]*raftTransportConn
}

func newRaftTransportFabric() *raftTransportFabric {
	return &raftTransportFabric{nodes: map[cluster.NodeID]*raftTransportConn{}}
}

func (f *raftTransportFabric) conn(id cluster.NodeID) *raftTransportConn {
	c := &raftTransportConn{id: id, fabric: f, receivers: map[internode.Class]func(cluster.NodeID, []byte){}}
	f.nodes[id] = c
	return c
}

type raftTransportConn struct {
	receivers map[internode.Class]func(cluster.NodeID, []byte)
	fabric    *raftTransportFabric
	id        cluster.NodeID
}

func (c *raftTransportConn) Start(context.Context, func(cluster.NodeID, []byte)) error { return nil }
func (c *raftTransportConn) Stop() error                                               { return nil }
func (c *raftTransportConn) SendToNode(nodeID cluster.NodeID, data []byte, class internode.Class) error {
	peer := c.fabric.nodes[nodeID]
	if peer == nil {
		return fmt.Errorf("unknown peer %s", nodeID)
	}
	recv := peer.receivers[class]
	if recv == nil {
		return fmt.Errorf("no receiver for class %s", class)
	}
	cp := append([]byte(nil), data...)
	recv(c.id, cp)
	return nil
}
func (c *raftTransportConn) EnsureConnection(cluster.NodeID, string, int)     {}
func (c *raftTransportConn) DisconnectFromNode(cluster.NodeID)                {}
func (c *raftTransportConn) ConnectedNodes() []cluster.NodeID                 { return nil }
func (c *raftTransportConn) GetListenPort() int                               { return 0 }
func (c *raftTransportConn) AddManagedNode(cluster.NodeID)                    {}
func (c *raftTransportConn) RemoveManagedNode(cluster.NodeID)                 {}
func (c *raftTransportConn) IsManaged(cluster.NodeID) bool                    { return true }
func (c *raftTransportConn) EvictOrphanNodes(map[cluster.NodeID]struct{}) int { return 0 }
func (c *raftTransportConn) RecordDropReason(string)                          {}
func (c *raftTransportConn) RegisterClassReceiver(class internode.Class, recv func(cluster.NodeID, []byte)) bool {
	if recv != nil && c.receivers[class] != nil {
		return false
	}
	c.receivers[class] = recv
	return true
}

var _ internode.ConnectionManager = (*raftTransportConn)(nil)

func TestRaftMessageTransport_RequestReply(t *testing.T) {
	fabric := newRaftTransportFabric()
	aConn := fabric.conn("node-a")
	bConn := fabric.conn("node-b")

	a, err := newRaftMessageTransport("node-a", aConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer a.Close()
	b, err := newRaftMessageTransport("node-b", bConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer b.Close()

	go func() {
		rpc := <-b.Consumer()
		req := rpc.Command.(*hraft.RequestVoteRequest)
		rpc.Respond(&hraft.RequestVoteResponse{Term: req.Term, Granted: true}, nil)
	}()

	resp := new(hraft.RequestVoteResponse)
	err = a.RequestVote("node-b", "node-b", &hraft.RequestVoteRequest{Term: 7}, resp)
	require.NoError(t, err)
	require.Equal(t, uint64(7), resp.Term)
	require.True(t, resp.Granted)
}

func TestRaftMessageTransport_InstallSnapshotCarriesBytes(t *testing.T) {
	fabric := newRaftTransportFabric()
	aConn := fabric.conn("node-a")
	bConn := fabric.conn("node-b")

	a, err := newRaftMessageTransport("node-a", aConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer a.Close()
	b, err := newRaftMessageTransport("node-b", bConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer b.Close()

	go func() {
		rpc := <-b.Consumer()
		data, readErr := io.ReadAll(rpc.Reader)
		require.NoError(t, readErr)
		require.Equal(t, []byte("snapshot-data"), data)
		rpc.Respond(&hraft.InstallSnapshotResponse{Term: 3, Success: true}, nil)
	}()

	resp := new(hraft.InstallSnapshotResponse)
	err = a.InstallSnapshot("node-b", "node-b", &hraft.InstallSnapshotRequest{Term: 3, Size: int64(len("snapshot-data"))}, resp, bytesReader("snapshot-data"))
	require.NoError(t, err)
	require.True(t, resp.Success)
}

func TestRaftMessageTransport_InstallSnapshotChunksBytes(t *testing.T) {
	fabric := newRaftTransportFabric()
	aConn := fabric.conn("node-a")
	bConn := fabric.conn("node-b")

	a, err := newRaftMessageTransport("node-a", aConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer a.Close()
	b, err := newRaftMessageTransport("node-b", bConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer b.Close()

	snapshot := bytes.Repeat([]byte("x"), raftSnapshotChunkSize*3+17)
	go func() {
		rpc := <-b.Consumer()
		data, readErr := io.ReadAll(rpc.Reader)
		require.NoError(t, readErr)
		require.Equal(t, snapshot, data)
		rpc.Respond(&hraft.InstallSnapshotResponse{Term: 3, Success: true}, nil)
	}()

	resp := new(hraft.InstallSnapshotResponse)
	err = a.InstallSnapshot("node-b", "node-b", &hraft.InstallSnapshotRequest{Term: 3, Size: int64(len(snapshot))}, resp, bytes.NewReader(snapshot))
	require.NoError(t, err)
	require.True(t, resp.Success)
}

func TestRaftMessageTransport_InstallSnapshotStreamTimesOutWhenChunksStop(t *testing.T) {
	fabric := newRaftTransportFabric()
	bConn := fabric.conn("node-b")

	b, err := newRaftMessageTransport("node-b", bConn, 20*time.Millisecond, zap.NewNop())
	require.NoError(t, err)
	defer b.Close()

	payload, err := encodeMsgpack(&hraft.InstallSnapshotRequest{Term: 3, Size: int64(raftSnapshotChunkSize)})
	require.NoError(t, err)
	wire, err := encodeMsgpack(&raftFrame{ID: 42, Type: raftRPCInstallSnapshot, Request: true, Payload: payload})
	require.NoError(t, err)

	b.onFrame("node-a", wire)
	rpc := <-b.Consumer()
	_, err = io.ReadAll(rpc.Reader)
	require.ErrorContains(t, err, "snapshot stream timed out")
}

func TestRaftMessageTransport_RPCTimeoutAndLateReplyDoesNotPoisonNextRPC(t *testing.T) {
	fabric := newRaftTransportFabric()
	aConn := fabric.conn("node-a")
	bConn := fabric.conn("node-b")

	a, err := newRaftMessageTransport("node-a", aConn, 20*time.Millisecond, zap.NewNop())
	require.NoError(t, err)
	defer a.Close()
	b, err := newRaftMessageTransport("node-b", bConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer b.Close()

	firstSeen := make(chan struct{})
	go func() {
		rpc := <-b.Consumer()
		close(firstSeen)
		time.Sleep(75 * time.Millisecond)
		rpc.Respond(&hraft.RequestVoteResponse{Term: 1, Granted: true}, nil)
	}()

	resp := new(hraft.RequestVoteResponse)
	err = a.RequestVote("node-b", "node-b", &hraft.RequestVoteRequest{Term: 1}, resp)
	require.ErrorContains(t, err, "timed out")
	<-firstSeen

	go func() {
		rpc := <-b.Consumer()
		req := rpc.Command.(*hraft.RequestVoteRequest)
		rpc.Respond(&hraft.RequestVoteResponse{Term: req.Term, Granted: true}, nil)
	}()

	resp = new(hraft.RequestVoteResponse)
	err = a.RequestVote("node-b", "node-b", &hraft.RequestVoteRequest{Term: 2}, resp)
	require.NoError(t, err)
	require.Equal(t, uint64(2), resp.Term)
	require.True(t, resp.Granted)
}

func TestRaftMessageTransport_AppendEntriesPipeline(t *testing.T) {
	fabric := newRaftTransportFabric()
	aConn := fabric.conn("node-a")
	bConn := fabric.conn("node-b")

	a, err := newRaftMessageTransport("node-a", aConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer a.Close()
	b, err := newRaftMessageTransport("node-b", bConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer b.Close()

	go func() {
		rpc := <-b.Consumer()
		req := rpc.Command.(*hraft.AppendEntriesRequest)
		rpc.Respond(&hraft.AppendEntriesResponse{Term: req.Term, Success: true}, nil)
	}()

	pipeline, err := a.AppendEntriesPipeline("node-b", "node-b")
	require.NoError(t, err)
	defer pipeline.Close()

	resp := new(hraft.AppendEntriesResponse)
	fut, err := pipeline.AppendEntries(&hraft.AppendEntriesRequest{Term: 11}, resp)
	require.NoError(t, err)

	select {
	case done := <-pipeline.Consumer():
		require.Same(t, fut, done)
		require.NoError(t, done.Error())
		require.True(t, done.Response().Success)
		require.Equal(t, uint64(11), done.Response().Term)
	case <-time.After(time.Second):
		t.Fatal("pipeline future did not complete")
	}
}

func TestRaftMessageTransport_HeartbeatFastPath(t *testing.T) {
	fabric := newRaftTransportFabric()
	aConn := fabric.conn("node-a")
	bConn := fabric.conn("node-b")

	a, err := newRaftMessageTransport("node-a", aConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer a.Close()
	b, err := newRaftMessageTransport("node-b", bConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer b.Close()

	var fastPath atomic.Int32
	b.SetHeartbeatHandler(func(rpc hraft.RPC) {
		fastPath.Add(1)
		rpc.Respond(&hraft.AppendEntriesResponse{Term: 4, Success: true}, nil)
	})

	resp := new(hraft.AppendEntriesResponse)
	err = a.AppendEntries("node-b", "node-b", &hraft.AppendEntriesRequest{
		RPCHeader: hraft.RPCHeader{Addr: []byte("node-a")},
		Term:      4,
	}, resp)
	require.NoError(t, err)
	require.Equal(t, int32(1), fastPath.Load())
	require.True(t, resp.Success)

	select {
	case rpc := <-b.Consumer():
		t.Fatalf("heartbeat should not hit normal consumer: %T", rpc.Command)
	case <-time.After(25 * time.Millisecond):
	}

	b.SetHeartbeatHandler(nil)
	go func() {
		rpc := <-b.Consumer()
		rpc.Respond(&hraft.AppendEntriesResponse{Term: 5, Success: true}, nil)
	}()

	resp = new(hraft.AppendEntriesResponse)
	err = a.AppendEntries("node-b", "node-b", &hraft.AppendEntriesRequest{
		RPCHeader: hraft.RPCHeader{Addr: []byte("node-a")},
		Term:      5,
	}, resp)
	require.NoError(t, err)
	require.Equal(t, int32(1), fastPath.Load())
	require.True(t, resp.Success)
}

func TestRaftMessageTransport_InboundRPCLimitReturnsBusy(t *testing.T) {
	fabric := newRaftTransportFabric()
	aConn := fabric.conn("node-a")
	bConn := fabric.conn("node-b")

	a, err := newRaftMessageTransport("node-a", aConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer a.Close()
	b, err := newRaftMessageTransport("node-b", bConn, time.Second, zap.NewNop())
	require.NoError(t, err)
	defer b.Close()

	for i := 0; i < cap(b.inflight); i++ {
		b.inflight <- struct{}{}
	}

	resp := new(hraft.RequestVoteResponse)
	err = a.RequestVote("node-b", "node-b", &hraft.RequestVoteRequest{Term: 1}, resp)
	require.ErrorContains(t, err, "inbound rpc limit reached")
}

func bytesReader(s string) io.Reader { return strings.NewReader(s) }

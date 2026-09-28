// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"encoding/binary"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
)

type stalledForwardRaft struct {
	*fakeRaft
	entered  chan struct{}
	release  chan struct{}
	commands chan []byte
	calls    atomic.Int32
}

func (r *stalledForwardRaft) Apply(data []byte, _ time.Duration) (*raftapi.ApplyResponse, error) {
	r.calls.Add(1)
	r.entered <- struct{}{}
	<-r.release
	r.commands <- append([]byte(nil), data...)
	return &raftapi.ApplyResponse{Response: applyResult{OK: true}}, nil
}

type forwardReplyCapture struct{ replies chan []byte }

func (r *forwardReplyCapture) Send(pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)
	for _, msg := range pkg.Messages {
		if msg.Topic == topicKVForwardResp {
			r.replies <- append([]byte(nil), msg.Payloads[0].Data().([]byte)...)
		}
	}
	return nil
}

func newStalledForwardEngine(t *testing.T) (*RaftEngine, *stalledForwardRaft, *forwardReplyCapture) {
	t.Helper()
	r := &stalledForwardRaft{
		fakeRaft: &fakeRaft{leader: true},
		entered:  make(chan struct{}, maxForwardConcurrent),
		release:  make(chan struct{}),
		commands: make(chan []byte, maxForwardConcurrent),
	}
	router := &forwardReplyCapture{replies: make(chan []byte, maxForwardConcurrent+2)}
	e := NewRaftEngine(r, NewRaftFSM(), "leader", router, nil)
	require.NoError(t, e.Start(context.Background()))
	t.Cleanup(func() { close(r.release); require.NoError(t, e.Stop()) })
	return e, r, router
}

func dispatchTestForward(e *RaftEngine, corr uint64, data []byte) []byte {
	envelope := make([]byte, 9+len(data))
	binary.BigEndian.PutUint64(envelope, corr)
	copy(envelope[9:], data)
	pkg := relay.NewServicePackage("peer", KVRaftHostID, "leader", KVRaftHostID, topicKVForwardReq, payload.New(envelope))
	_ = e.Send(pkg)
	return envelope
}

func TestForwardDispatchDoesNotBlockReceiveAndOwnsBytes(t *testing.T) {
	e, r, router := newStalledForwardEngine(t)
	returned := make(chan []byte, 1)
	go func() { returned <- dispatchTestForward(e, 7, []byte("command")) }()
	var envelope []byte
	select {
	case envelope = <-returned:
	case <-time.After(time.Second):
		t.Fatal("receive callback blocked on Raft Apply")
	}
	select {
	case <-r.entered:
	case <-time.After(time.Second):
		t.Fatal("forwarded write did not reach Raft")
	}
	copy(envelope[9:], "mutated")
	// The permit remains held for the future even when Stop detaches it.
	stopped := make(chan struct{})
	go func() { _ = e.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked on non-cancelable Raft Apply")
	}
	require.Len(t, e.forwardSem, 1)
	dispatchTestForward(e, 8, []byte("closed"))
	require.EqualValues(t, 1, r.calls.Load(), "shutdown must reject new writes")
	response := <-router.replies
	require.Equal(t, uint64(8), binary.BigEndian.Uint64(response))
	require.Contains(t, string(response[18:]), "closed")
	// Unblock the future separately from cleanup to verify the detached input.
	r.release <- struct{}{}
	select {
	case command := <-r.commands:
		require.Equal(t, "command", string(command))
	case <-time.After(time.Second):
		t.Fatal("Raft future did not finish")
	}
	require.Eventually(t, func() bool { return len(e.forwardSem) == 0 }, time.Second, time.Millisecond)
	require.Empty(t, router.replies, "stopped operation must not send a late response")
}

func TestForwardDispatchOverloadIsBoundedAndRejected(t *testing.T) {
	e, r, router := newStalledForwardEngine(t)
	for i := range maxForwardConcurrent {
		dispatchTestForward(e, uint64(i), []byte("command"))
	}
	for range maxForwardConcurrent {
		select {
		case <-r.entered:
		case <-time.After(time.Second):
			t.Fatal("admitted write did not enter Raft")
		}
	}
	dispatchTestForward(e, 999, []byte("overload"))
	select {
	case response := <-router.replies:
		require.Equal(t, uint64(999), binary.BigEndian.Uint64(response))
		require.Equal(t, errOther, response[17])
		require.Equal(t, errForwardOverloaded.Error(), string(response[18:]))
	case <-time.After(time.Second):
		t.Fatal("overloaded request was not rejected")
	}
	require.EqualValues(t, maxForwardConcurrent, r.calls.Load())
	require.Len(t, e.forwardSem, maxForwardConcurrent)
}

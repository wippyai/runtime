// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
)

type shardReplySender func(string, []byte) error

func (f shardReplySender) Send(node string, frame []byte) error { return f(node, frame) }

func TestRejoin_JoinWhileTheInitialSendIsFailing(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan struct{}, 1)
	var calls atomic.Int32
	s := NewService(Config{LocalNodeID: "a", Sender: shardReplySender(func(string, []byte) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return errors.New("old membership view")
		}
		delivered <- struct{}{}
		return nil
	})})
	_, err := s.Register("svc", pid.PID{Node: "a", Host: "workers", UniqID: "owner"})
	require.NoError(t, err)
	request, err := EncodeShardRequestFrame("b", []uint16{uint16(ShardFor("svc"))})
	require.NoError(t, err)
	done := make(chan struct{})
	go func() { defer close(done); s.OnFrame(request) }()
	t.Cleanup(func() { close(release); <-done; _ = s.Stop() })
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("initial reply never entered Send")
	}
	// Membership can report a join before the failed Send returns. The join
	// must see the request without waiting for another anti-entropy round.
	s.onNodeJoinedEvent(event.Event{Data: cluster.NodeEvent{Node: cluster.NodeInfo{ID: "b"}}})
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("join missed the in-flight shard request")
	}
}

func TestRejoin_StopDoesNotRetainAnInflightReply(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	s := NewService(Config{LocalNodeID: "a", Sender: shardReplySender(func(string, []byte) error {
		close(entered)
		<-release
		return errors.New("not a member")
	})})
	_, err := s.Register("svc", pid.PID{Node: "a", Host: "workers", UniqID: "owner"})
	require.NoError(t, err)
	request, err := EncodeShardRequestFrame("b", []uint16{uint16(ShardFor("svc"))})
	require.NoError(t, err)
	done := make(chan struct{})
	go func() { defer close(done); s.OnFrame(request) }()
	<-entered
	require.NoError(t, s.Stop())
	close(release)
	<-done
	s.heldMu.Lock()
	defer s.heldMu.Unlock()
	require.Empty(t, s.held, "a stopped service must not retain responses")
}

func TestRejoin_PendingRequestersAreBounded(t *testing.T) {
	s := NewService(Config{LocalNodeID: "a", BroadcastCap: 2, Sender: shardReplySender(func(string, []byte) error {
		return errors.New("not a member")
	})})
	t.Cleanup(func() { _ = s.Stop() })
	_, err := s.Register("svc", pid.PID{Node: "a", Host: "workers", UniqID: "owner"})
	require.NoError(t, err)
	for i := range 10 {
		request, err := EncodeShardRequestFrame(fmt.Sprintf("peer-%d", i), []uint16{uint16(ShardFor("svc"))})
		require.NoError(t, err)
		s.OnFrame(request)
	}
	s.heldMu.Lock()
	defer s.heldMu.Unlock()
	require.LessOrEqual(t, len(s.held), 2)
}

func TestDecodeShardRequest_RejectsOverflowingCount(t *testing.T) {
	data := binary.LittleEndian.AppendUint16(nil, 32768)
	require.NotPanics(t, func() {
		_, err := DecodeShardRequest(data)
		require.Error(t, err)
	})
}

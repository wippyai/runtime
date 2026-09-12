// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/cluster/raft/multiplex"
)

type blockedForwardRaft struct {
	raftSubmitter
	entered chan struct{}
	release chan struct{}
}

func (r *blockedForwardRaft) Apply([]byte, time.Duration) (*raftapi.ApplyResponse, error) {
	close(r.entered)
	<-r.release
	return &raftapi.ApplyResponse{Response: applyResult{OK: true}}, nil
}

type discardForwardRouter struct{}

func (discardForwardRouter) Send(p *relay.Package) error { relay.ReleasePackage(p); return nil }

func TestForwardAdmissionKeepsRepliesMovingAndJoinsAcceptedWork(t *testing.T) {
	fsm := NewRaftFSM(nil)
	r := &blockedForwardRaft{raftSubmitter: &fakeRaft{fsm: fsm, leader: true}, entered: make(chan struct{}), release: make(chan struct{})}
	e := NewRaftEngine(r, fsm, nil, "authority", discardForwardRouter{}, nil)
	require.NoError(t, e.ConfigureForwarding(1))
	require.NoError(t, e.Start(context.Background()))
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(r.release) }); _ = e.Stop() })
	request := func() *relay.Package {
		body := make([]byte, 9)
		body = append(body, multiplex.KVDomain)
		body = append(body, encodeCommand(command{Op: opSet, Key: "k", Value: []byte("v")})...)
		return relay.NewServicePackage("client", KVRaftHostID, "authority", KVRaftHostID, topicKVForwardReq, payload.New(body))
	}
	sent := make(chan error, 1)
	go func() {
		p := request()
		err := e.Send(p)
		if err != nil {
			relay.ReleasePackage(p)
		}
		sent <- err
	}()
	select {
	case err := <-sent:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("request blocked the reader")
	}
	select {
	case <-r.entered:
	case <-time.After(time.Second):
		t.Fatal("request never reached Apply")
	}
	// An overloaded request is accepted only for a correlated refusal.
	require.NoError(t, e.Send(request()))
	reply := make(chan applyResult, 1)
	e.fwdMu.Lock()
	e.pending[77] = forwardReply[applyResult]{peer: "client", result: reply}
	e.fwdMu.Unlock()
	body := make([]byte, 18)
	binary.BigEndian.PutUint64(body[:8], 77)
	body[16] = 1
	require.NoError(t, e.Send(relay.NewServicePackage("client", KVRaftHostID, "authority", KVRaftHostID, topicKVForwardResp, payload.New(body))))
	select {
	case res := <-reply:
		require.True(t, res.OK)
	default:
		t.Fatal("reply stalled behind accepted request")
	}
	stopped := make(chan struct{})
	go func() { _ = e.Stop(); close(stopped) }()
	select {
	case <-e.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel engine")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned with Apply still running")
	default:
	}
	late := request()
	err := e.Send(late)
	require.ErrorContains(t, err, "stopped")
	relay.ReleasePackage(late)
	release.Do(func() { close(r.release) })
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not join released worker")
	}
	require.Empty(t, e.forwardSlots)
}

type cancelForwardRouter struct{ entered chan struct{} }

func (r *cancelForwardRouter) Send(*relay.Package) error {
	panic("context-aware transport must use SendContext")
}
func (r *cancelForwardRouter) SendContext(ctx context.Context, _ *relay.Package) error {
	close(r.entered)
	<-ctx.Done()
	return ctx.Err()
}

func TestForwardStopCancelsBlockedResponseDelivery(t *testing.T) {
	fsm := NewRaftFSM(nil)
	router := &cancelForwardRouter{entered: make(chan struct{})}
	e := NewRaftEngine(&fakeRaft{fsm: fsm, leader: true}, fsm, nil, "authority", router, nil)
	require.NoError(t, e.Start(context.Background()))
	t.Cleanup(func() { _ = e.Stop() })
	body := make([]byte, 9)
	// An invalid command elicits a response without applying any mutation.
	p := relay.NewServicePackage("client", KVRaftHostID, "authority", KVRaftHostID, topicKVForwardReq, payload.New(body))
	require.NoError(t, e.Send(p))
	select {
	case <-router.entered:
	case <-time.After(time.Second):
		t.Fatal("response never entered transport")
	}
	stopped := make(chan struct{})
	go func() { _ = e.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked on response delivery")
	}
	require.Empty(t, e.forwardSlots)
}

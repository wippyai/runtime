// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type damagedWriteReplyRouter struct {
	*routerTo
	damage func([]byte) []byte
}

func (r damagedWriteReplyRouter) Send(pkg *relay.Package) error {
	if pkg.Messages[0].Topic == topicKVForwardResp {
		p := pkg.Messages[0].Payloads[0]
		original := p.Data().([]byte)
		// Mutate only the response after the leader has actually applied the write.
		altered := r.damage(bytes.Clone(original))
		// A replacement payload also supports truncated/extended frames.
		pkg.Messages[0].Payloads[0] = payload.New(altered)
	}
	return r.routerTo.Send(pkg)
}
func (r damagedWriteReplyRouter) SendContext(ctx context.Context, pkg *relay.Package) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Send(pkg)
}

func TestMalformedWriteReplyNeverRetriesCommittedMutation(t *testing.T) {
	for _, shape := range []string{"generic-lookalike", "success-notleader", "notleader-trailing", "invalid-ok", "short-header"} {
		t.Run(shape, func(t *testing.T) {
			router := damagedWriteReplyRouter{routerTo: &routerTo{}, damage: func(out []byte) []byte {
				switch shape {
				case "generic-lookalike":
					clear(out[8:17])
					out[17] = errOther
					return append(out[:18], []byte(errForwardNotLeader.Error())...)
				case "success-notleader":
					out[17] = errNotLeaderCode
				case "notleader-trailing":
					clear(out[8:17])
					out[17] = errNotLeaderCode
					return append(out, 1)
				case "invalid-ok":
					out[16] = 2
				case "short-header":
					return out[:8]
				}
				return out
			}}
			router.engines = make(map[string]*RaftEngine)
			for _, node := range []string{"leader", "client"} {
				fsm := NewRaftFSM(nil)
				e := NewRaftEngine(&fakeRaft{fsm: fsm, leader: node == "leader", leaderID: "leader"}, fsm, nil, node, router, nil)
				e.forwardWait = 100 * time.Millisecond
				router.engines[node] = e
				require.NoError(t, e.Start(t.Context()))
				t.Cleanup(func() { require.NoError(t, e.Stop()) })
			}
			_, err := router.engines["client"].Set("once", []byte("value"))
			require.Error(t, err)
			value, found := router.engines["leader"].fsm.get("once")
			require.True(t, found)
			require.Equal(t, uint64(1), value.Version, "an invalid reply must not replay a committed write")
			require.NotErrorIs(t, err, errForwardNotLeader)
			require.NotErrorIs(t, err, errForwardTimeout, "a correlatable malformed reply must wake its waiter")
		})
	}
}

type writeShapeCaptureReplies struct{ replies chan []byte }

func (r writeShapeCaptureReplies) Send(pkg *relay.Package) error {
	return r.SendContext(context.Background(), pkg)
}
func (r writeShapeCaptureReplies) SendContext(ctx context.Context, pkg *relay.Package) error {
	data := bytes.Clone(pkg.Messages[0].Payloads[0].Data().([]byte))
	select {
	case r.replies <- data:
		relay.ReleasePackage(pkg)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestForwardWriteReplyCanonicalRoundTrip(t *testing.T) {
	cases := []applyResult{
		{}, {OK: true}, {OK: true, Version: 7}, {Version: 9},
		{Err: kvapi.ErrKeyNotFound}, {Err: kvapi.ErrLeaseNotFound},
		{Err: kvapi.ErrVersionMismatch, Version: 11}, {Err: raftapi.ErrNotLeader},
		{Err: raftapi.ErrLeadershipLost}, {Err: errors.New("")},
		{Err: errors.New(errForwardNotLeader.Error())},
	}
	for _, result := range cases {
		replies := make(chan []byte, 1)
		fsm := NewRaftFSM(nil)
		e := NewRaftEngine(&fakeRaft{fsm: fsm, leader: true}, fsm, nil, "leader", writeShapeCaptureReplies{replies: replies}, nil)
		require.NoError(t, e.Start(t.Context()))
		e.replyForward("client", 42, result)
		var out []byte
		select {
		case out = <-replies:
		case <-time.After(time.Second):
			t.Fatal("write reply missing")
		}
		require.NoError(t, e.Stop())
		require.Equal(t, uint64(42), binary.BigEndian.Uint64(out))
		decoded := decodeForwardWriteResponse(out)
		require.Equal(t, result.OK, decoded.OK)
		require.Equal(t, result.Version, decoded.Version)
		if errors.Is(result.Err, raftapi.ErrNotLeader) {
			require.ErrorIs(t, decoded.Err, errForwardNotLeader)
		} else if result.Err == nil {
			require.NoError(t, decoded.Err)
		} else {
			require.EqualError(t, decoded.Err, result.Err.Error())
			require.NotErrorIs(t, decoded.Err, errForwardNotLeader)
		}
	}
}

func FuzzForwardWriteReplyRetryClassification(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 18))
	rejection := make([]byte, 18)
	rejection[17] = errNotLeaderCode
	f.Add(rejection)
	opaque := make([]byte, 18)
	opaque[17] = errOther
	f.Add(append(opaque, []byte(errForwardNotLeader.Error())...))
	f.Fuzz(func(t *testing.T, out []byte) {
		if len(out) > 4096 {
			t.Skip()
		}
		result := decodeForwardWriteResponse(out)
		if errors.Is(result.Err, errForwardNotLeader) {
			require.Len(t, out, 18)
			require.Equal(t, byte(0), out[16])
			require.Equal(t, errNotLeaderCode, out[17])
			require.Zero(t, binary.BigEndian.Uint64(out[8:16]))
		}
		if result.Err == nil {
			require.Len(t, out, 18)
			require.LessOrEqual(t, out[16], byte(1))
			require.Equal(t, errNone, out[17])
		}
	})
}

type rejectFirstWriteRouter struct {
	*routerTo
	rejected atomic.Bool
}

func (r *rejectFirstWriteRouter) Send(pkg *relay.Package) error {
	if pkg.Messages[0].Topic == topicKVForwardReq && r.rejected.CompareAndSwap(false, true) {
		response := make([]byte, 18)
		copy(response[:8], pkg.Messages[0].Payloads[0].Data().([]byte)[:8])
		response[17] = errNotLeaderCode
		reply := relay.NewServicePackage(pkg.Target.Node, KVRaftHostID, pkg.Source.Node, KVRaftHostID, topicKVForwardResp, payload.New(response))
		relay.ReleasePackage(pkg)
		return r.routerTo.Send(reply)
	}
	return r.routerTo.Send(pkg)
}
func (r *rejectFirstWriteRouter) SendContext(ctx context.Context, pkg *relay.Package) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Send(pkg)
}
func TestExplicitWriteRejectionStillRetries(t *testing.T) {
	router := &rejectFirstWriteRouter{routerTo: &routerTo{engines: make(map[string]*RaftEngine)}}
	for _, node := range []string{"leader", "client"} {
		fsm := NewRaftFSM(nil)
		e := NewRaftEngine(&fakeRaft{fsm: fsm, leader: node == "leader", leaderID: "leader"}, fsm, nil, node, router, nil)
		router.engines[node] = e
		require.NoError(t, e.Start(t.Context()))
		t.Cleanup(func() { require.NoError(t, e.Stop()) })
	}
	version, err := router.engines["client"].Set("once", []byte("value"))
	require.NoError(t, err)
	require.True(t, router.rejected.Load())
	require.Equal(t, uint64(1), version)
	value, found := router.engines["leader"].fsm.get("once")
	require.True(t, found)
	require.Equal(t, uint64(1), value.Version)
}

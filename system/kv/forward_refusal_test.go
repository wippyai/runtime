// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type stalledRefusalRouter struct{ entered chan []byte }

func (*stalledRefusalRouter) Send(*relay.Package) error { panic("expected cancellable send") }
func (r *stalledRefusalRouter) SendContext(ctx context.Context, p *relay.Package) error {
	body := p.Messages[0].Payloads[0].Data().([]byte)
	select {
	case r.entered <- append([]byte(nil), body...):
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestForwardRefusalBoundedCorrelatedAndCancelable(t *testing.T) {
	for _, topic := range []relay.Topic{topicKVForwardReq, topicKVReadReq} {
		t.Run(string(topic), func(t *testing.T) {
			fsm := NewRaftFSM(nil)
			router := &stalledRefusalRouter{entered: make(chan []byte, 2)}
			e := NewRaftEngine(&fakeRaft{fsm: fsm, leader: true}, fsm, nil, "authority", router, nil)
			require.NoError(t, e.ConfigureForwarding(1))
			require.NoError(t, e.Start(context.Background()))
			t.Cleanup(func() { _ = e.Stop() })
			// Occupy the sole execution budget to exercise only refusal dispatch.
			e.forwardSlots <- struct{}{}
			var release sync.Once
			defer release.Do(func() { <-e.forwardSlots })
			request := func(corr uint64) *relay.Package {
				body := make([]byte, 9+1024*1024)
				binary.BigEndian.PutUint64(body[:8], corr)
				return relay.NewServicePackage("client", KVRaftHostID, "authority", KVRaftHostID, topic, payload.New(body))
			}
			require.NoError(t, e.Send(request(51)))
			var body []byte
			select {
			case body = <-router.entered:
			case <-time.After(time.Second):
				t.Fatal("no correlated refusal")
			}
			require.Equal(t, uint64(51), binary.BigEndian.Uint64(body[:8]))
			if topic == topicKVForwardReq {
				result := decodeForwardWriteResponse(body)
				require.ErrorIs(t, result.Err, kvapi.ErrOverloaded)
				require.False(t, result.OK)
			} else {
				ch := make(chan readResult, 1)
				e.pendingReads[51] = forwardReply[readResult]{peer: "client", result: ch}
				require.NoError(t, e.Send(relay.NewServicePackage("client", KVRaftHostID, "authority", KVRaftHostID, topicKVReadResp, payload.New(body))))
				select {
				case res := <-ch:
					require.ErrorIs(t, res.err, kvapi.ErrOverloaded)
					require.False(t, res.found)
				default:
					t.Fatal("no decoded read refusal")
				}
			}
			require.NoError(t, e.Send(request(52)), "one compact refusal fits while writer is blocked")
			extra := request(53)
			require.ErrorIs(t, e.Send(extra), kvapi.ErrOverloaded)
			require.Equal(t, "client", extra.Source.Node, "failed admission retained caller ownership")
			relay.ReleasePackage(extra)
			require.Len(t, e.forwardRefusals, 1)
			release.Do(func() { <-e.forwardSlots })
			stopped := make(chan struct{})
			go func() { _ = e.Stop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("Stop failed to cancel refusal writer")
			}
		})
	}
}

func TestForwardOverloadSurvivesProxyWithoutApplying(t *testing.T) {
	engines := startForwardCluster(t, map[string]string{"A": "A", "B": "A", "C": "B"})
	authority := engines["A"]
	// Exhaust execution capacity without admitting an actual write.
	for i := 0; i < cap(authority.forwardSlots); i++ {
		authority.forwardSlots <- struct{}{}
	}
	defer func() {
		for len(authority.forwardSlots) > 0 {
			<-authority.forwardSlots
		}
	}()
	_, err := engines["C"].Set("never-applied", []byte("value"))
	require.ErrorIs(t, err, kvapi.ErrOverloaded)
	_, err = authority.Get("never-applied")
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	_, err = engines["C"].GetViaLeader("never-applied")
	require.ErrorIs(t, err, kvapi.ErrOverloaded, "read refusal must survive the intermediary")
}

func TestForwardOverloadRejectsContradictoryVersion(t *testing.T) {
	body := make([]byte, 18)
	body[17] = errOverloadedCode
	binary.BigEndian.PutUint64(body[8:16], 1)
	res := decodeForwardWriteResponse(body)
	require.Error(t, res.Err)
	require.NotErrorIs(t, res.Err, kvapi.ErrOverloaded, "malformed response must not claim a safe refusal")
}

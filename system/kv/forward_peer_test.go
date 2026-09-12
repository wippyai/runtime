// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
)

func TestForwardRepliesRequireExpectedPeer(t *testing.T) {
	for _, read := range []bool{false, true} {
		t.Run(map[bool]string{false: "write", true: "read"}[read], func(t *testing.T) {
			fsm := NewRaftFSM(nil)
			engine := NewRaftEngine(&fakeRaft{fsm: fsm, leaderID: "authority"}, fsm, nil, "client", nil, nil)
			writes := make(chan applyResult, 1)
			reads := make(chan readResult, 1)
			const corr uint64 = 42
			engine.pending[corr] = forwardReply[applyResult]{peer: "authority", result: writes}
			engine.pendingReads[corr] = forwardReply[readResult]{peer: "authority", result: reads}
			topic := topicKVForwardResp
			body := make([]byte, 18)
			binary.BigEndian.PutUint64(body[:8], corr)
			binary.BigEndian.PutUint64(body[8:16], 1)
			body[16] = 1
			if read {
				topic = topicKVReadResp
				body = make([]byte, 29)
				binary.BigEndian.PutUint64(body[:8], corr)
				body[8] = 1
				binary.BigEndian.PutUint64(body[9:17], 1)
				binary.BigEndian.PutUint64(body[17:25], 1)
			}
			send := func(source, host, peer string) {
				pkg := relay.NewServicePackage(source, host, "client", KVRaftHostID, topic, payload.New(body))
				pkg.ReceivedFrom = peer
				require.NoError(t, engine.Send(pkg))
			}
			for _, forged := range []struct{ source, host, peer string }{
				{"attacker", KVRaftHostID, "attacker"},
				{"authority", KVRaftHostID, "attacker"},
				{"authority", "unrelated-service", "authority"},
				{"attacker", KVRaftHostID, "authority"},
			} {
				send(forged.source, forged.host, forged.peer)
				require.Empty(t, writes, "forged reply completed a write")
				require.Empty(t, reads, "forged reply completed a read")
			}
			send("authority", KVRaftHostID, "authority")
			if read {
				require.Len(t, reads, 1)
				require.True(t, (<-reads).found)
			} else {
				require.Len(t, writes, 1)
				require.True(t, (<-writes).OK)
			}
		})
	}
}

// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"encoding/binary"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/cluster/raft/multiplex"
)

type countingDomainRaft struct {
	raftSubmitter
	applies int
}

func (r *countingDomainRaft) Apply(data []byte, timeout time.Duration) (*raftapi.ApplyResponse, error) {
	r.applies++
	return r.raftSubmitter.Apply(data, timeout)
}

func TestKVForwardRejectsOtherRaftDomainsBeforeApplyOrProxy(t *testing.T) {
	for _, target := range []string{"A", "B"} {
		t.Run(target, func(t *testing.T) {
			engines := startForwardCluster(t, map[string]string{"A": "A", "B": "A", "C": "B"})
			counted := &countingDomainRaft{raftSubmitter: engines["A"].raft}
			engines["A"].raft = counted
			for _, body := range [][]byte{nil, {multiplex.KVDomain}, {0x80}, {0x01, 0x02}} {
				result, err := engines["C"].sendForward(target, body, 0)
				require.NoError(t, err)
				require.EqualError(t, result.Err, "kv: invalid forwarded command domain")
				require.False(t, result.OK)
			}
			require.Zero(t, counted.applies, "invalid forwarding reached shared Raft Apply")
			// An ordinary KV operation still reaches the authority through B.
			_, err := engines["C"].Set("ordinary", []byte("value"))
			require.NoError(t, err)
			require.Equal(t, 1, counted.applies)
		})
	}
}

func TestKVForwardRejectsApplicationHostAndForgedNode(t *testing.T) {
	fsm := NewRaftFSM(nil)
	counted := &countingDomainRaft{raftSubmitter: &fakeRaft{fsm: fsm, leader: true, leaderID: "authority"}}
	authority := NewRaftEngine(counted, fsm, nil, "authority", nil, nil)
	body := make([]byte, 9)
	binary.BigEndian.PutUint64(body[:8], 42)
	body = append(body, multiplex.KVDomain)
	body = append(body, encodeCommand(command{Op: opSet, Key: "control", Value: []byte("forged")})...)
	for _, from := range []struct{ node, host, peer string }{
		{"client", "process-host", "client"},
		{"victim", KVRaftHostID, "attacker"},
	} {
		pkg := relay.NewServicePackage(from.node, from.host, "authority", KVRaftHostID, topicKVForwardReq, payload.New(body))
		pkg.ReceivedFrom = from.peer
		require.NoError(t, authority.Send(pkg))
	}
	require.Zero(t, counted.applies)
}

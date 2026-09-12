// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
)

type truncatedReadRouter struct {
	receiver *RaftEngine
	fallback relay.Receiver
}

func (r *truncatedReadRouter) Send(pkg *relay.Package) error {
	if pkg.Messages[0].Topic != topicKVReadReq && r.fallback != nil {
		return r.fallback.Send(pkg)
	}
	defer relay.ReleasePackage(pkg)
	for _, msg := range pkg.Messages {
		if msg.Topic != topicKVReadReq {
			continue
		}
		req := msg.Payloads[0].Data().([]byte)
		reply := make([]byte, 29)
		copy(reply[:8], req[:8])
		reply[8] = 1 // Found, but the declared value is missing from the frame.
		binary.BigEndian.PutUint32(reply[25:29], 10)
		return r.receiver.Send(relay.NewServicePackage(pkg.Target.Node, KVRaftHostID, pkg.Source.Node, KVRaftHostID, topicKVReadResp, payload.New(reply)))
	}
	return nil
}

func TestForwardReadRejectsTruncatedFoundResponse(t *testing.T) {
	fsm := NewRaftFSM(nil)
	router := &truncatedReadRouter{}
	eng := NewRaftEngine(&fakeRaft{fsm: fsm, leaderID: "leader"}, fsm, nil, "client", router, nil)
	router.receiver = eng
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop() })
	got, err := eng.GetViaLeader("present")
	if err == nil {
		t.Fatalf("truncated response accepted as a successful read: %+v", got)
	}
	if err.Error() != "kv: read response truncated" {
		t.Fatalf("lost protocol error: %v", err)
	}
}

func TestForwardReadRejectsTruncationThroughMember(t *testing.T) {
	engines := startForwardCluster(t, map[string]string{"A": "A", "B": "A", "C": "B"})
	member := engines["B"]
	member.router = &truncatedReadRouter{receiver: member, fallback: member.router}
	got, err := engines["C"].GetViaLeader("present")
	// The existing wire can only express unavailable at the intermediary.
	// It must never translate the corrupt value into success or not-found.
	if !errors.Is(err, errNoForwardLeader) {
		t.Fatalf("re-forwarded corrupt read = %+v, %v; want unavailable", got, err)
	}
}

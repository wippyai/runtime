// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
)

func TestE2E_KVRegistry_PendingRewriteRetainsParticipantVotes(t *testing.T) {
	if testing.Short() {
		t.Skip("real three-node raft registry test")
	}
	c := NewCluster(t, 3)
	leader := c.Leader()
	participants := []*Node{leader}
	var members []pid.NodeID
	for _, n := range c.Nodes() {
		members = append(members, n.ID)
		if n != leader && len(participants) == 1 {
			participants = append(participants, n)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	regs := make(map[string]*kvbacked.Service)
	for _, n := range participants {
		node := n
		r := kvbacked.NewService(node.KV, node.ID, nil, nil)
		r.ConfigureStrong(kvbacked.StrongDeps{
			Membership: func() []pid.NodeID { return members },
			IsLeader:   node.Raft.IsLeader,
			Deadline:   6 * time.Second,
		})
		if err := r.StartReconciler(ctx); err != nil {
			t.Fatalf("start registry on %s: %v", node.ID, err)
		}
		regs[node.ID] = r
	}
	owner := pid.PID{Node: participants[1].ID, Host: "proc", UniqID: "rewrite"}
	type result struct {
		err error
		out globalapi.RegisterOutcome
	}
	done := make(chan result, 1)
	go func() {
		out, err := regs[participants[1].ID].RegisterScope(ctx, "rewrite", owner, globalapi.Strong)
		done <- result{out: out, err: err}
	}()

	key := "_sys:registry:pending:rewrite"
	var entry kvapi.Entry
	var header struct {
		PID              string       `codec:"p"`
		Name             string       `codec:"n"`
		AttemptID        string       `codec:"a"`
		NodeID           pid.NodeID   `codec:"d"`
		RequiredNodes    []pid.NodeID `codec:"r"`
		DeadlineUnixNano int64        `codec:"dl"`
		CreatedAt        int64        `codec:"c"`
	}
	limit := time.Now().Add(4 * time.Second)
	observed := false
	for time.Now().Before(limit) {
		var err error
		entry, err = leader.KV.Get(key)
		if err == nil {
			if err = codec.NewDecoderBytes(entry.Value, &codec.MsgpackHandle{}).Decode(&header); err != nil {
				t.Fatalf("decode pending: %v", err)
			}
			if header.AttemptID != "" {
				both := true
				for _, n := range participants {
					if _, err := leader.KV.Get("_sys:registry:ack:rewrite:" + header.AttemptID + ":" + n.ID); err != nil {
						both = false
					}
				}
				if both {
					observed = true
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !observed {
		t.Fatal("required participant votes not observed before rewrite")
	}
	firstEpoch := entry.Epoch
	w, err := leader.KV.Watch(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	header.RequiredNodes = []pid.NodeID{participants[0].ID, participants[1].ID}
	var value []byte
	if err := codec.NewEncoderBytes(&value, &codec.MsgpackHandle{}).Encode(header); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := leader.KV.CompareAndSwap(key, entry.Version, value); err != nil || !ok {
		t.Fatalf("rewrite pending: updated=%v err=%v", ok, err)
	}
	select {
	case ev := <-w.Events():
		if ev.Current == nil || ev.Current.Epoch <= firstEpoch {
			t.Fatalf("pending rewrite must advance Raft epoch: before=%d event=%+v", firstEpoch, ev)
		}
	case <-w.Done():
		t.Fatalf("pending observation invalid: %v", w.Err())
	case <-time.After(4 * time.Second):
		t.Fatal("pending rewrite was not observed")
	}
	select {
	case got := <-done:
		if got.err != nil || got.out.State != globalapi.RegisterStateActive || !got.out.PID.Equal(owner) {
			t.Fatalf("committed votes lost across pending rewrite: %+v, %v", got.out, got.err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("committed votes were stranded by pending rewrite")
	}
}

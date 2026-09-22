// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/pid"
	systemkv "github.com/wippyai/runtime/system/kv"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
)

type legacySnapshotSink struct{ bytes.Buffer }

func (*legacySnapshotSink) ID() string    { return "legacy-registry" }
func (*legacySnapshotSink) Cancel() error { return nil }
func (*legacySnapshotSink) Close() error  { return nil }

func TestE2E_KVRegistry_LegacyPendingRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("real raft durability test")
	}
	c := NewCluster(t, 1)
	n := c.Node(0)
	owner := pid.PID{Node: n.ID, Host: "proc", UniqID: "legacy"}
	const key = "_sys:registry:pending:legacy"
	var data []byte
	if err := codec.NewEncoderBytes(&data, &codec.MsgpackHandle{}).Encode(map[string]any{
		"p": owner.String(), "n": "legacy", "d": n.ID,
		"r": []string{n.ID, "peer"}, "dl": time.Now().Add(time.Minute).UnixNano(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := n.KV.Set(key, data); err != nil {
		t.Fatal(err)
	}
	before, err := n.KV.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	ack := fmt.Sprintf("_sys:registry:ack:legacy:%d:%s", before.Epoch, n.ID)
	if _, err := n.KV.Set(ack, []byte(n.ID)); err != nil {
		t.Fatal(err)
	}
	beforeVote, err := n.KV.Get(ack)
	if err != nil {
		t.Fatal(err)
	}
	// Recover a saved FSM snapshot with a fresh registry, without any migration
	// writes, then separately recover the live cluster through its durable log.
	snapshot, err := n.KVFSM.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	sink := &legacySnapshotSink{}
	if err := snapshot.Persist(sink); err != nil {
		t.Fatal(err)
	}
	snapshot.Release()
	fsm := systemkv.NewRaftFSM()
	if err := fsm.Restore(io.NopCloser(bytes.NewReader(sink.Bytes()))); err != nil {
		t.Fatal(err)
	}
	reader := systemkv.NewRaftEngine(nil, fsm, n.ID, nil, nil)
	recovered := kvbacked.NewService(reader, n.ID, nil, nil)
	recovered.ConfigureStrong(kvbacked.StrongDeps{IsLeader: func() bool { return false }})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	if err := recovered.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	if got, held := recovered.IsStrongReserved("legacy"); !held || !got.Equal(owner) {
		t.Fatalf("snapshot lost legacy exclusion: %v %v", got, held)
	}
	fromSnapshot, err := reader.Get(key)
	if err != nil || fromSnapshot.Version != before.Version || fromSnapshot.Epoch != before.Epoch || !bytes.Equal(fromSnapshot.Value, data) {
		t.Fatalf("snapshot changed legacy pending: %+v err=%v", fromSnapshot, err)
	}
	c.Kill(0)
	c.Restart(0)
	c.WaitLeader(10 * time.Second)
	n = c.Node(0)
	after, err := n.KV.Get(key)
	if err != nil || after.Version != before.Version || after.Epoch != before.Epoch || !bytes.Equal(after.Value, data) {
		t.Fatalf("log recovery changed pending: %+v err=%v", after, err)
	}
	reg := kvbacked.NewService(n.KV, n.ID, nil, nil)
	reg.ConfigureStrong(kvbacked.StrongDeps{IsLeader: func() bool { return false }})
	if err := reg.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	if got, held := reg.IsStrongReserved("legacy"); !held || !got.Equal(owner) {
		t.Fatalf("log recovery lost exclusion: %v %v", got, held)
	}
	afterVote, err := n.KV.Get(ack)
	if err != nil || afterVote.Version != beforeVote.Version || afterVote.Epoch != beforeVote.Epoch || !bytes.Equal(afterVote.Value, beforeVote.Value) {
		t.Fatalf("recovery changed old vote: %+v err=%v", afterVote, err)
	}
	// The missing vote still uses the original pending epoch after restart.
	peerAck := fmt.Sprintf("_sys:registry:ack:legacy:%d:peer", before.Epoch)
	if _, err := n.KV.Set(peerAck, []byte("peer")); err != nil {
		t.Fatal(err)
	}
	leader := kvbacked.NewService(n.KV, n.ID, nil, nil)
	leader.ConfigureStrong(kvbacked.StrongDeps{IsLeader: n.Raft.IsLeader})
	if err := leader.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	waitLookup(t, leader, "legacy", owner, 5*time.Second)
	active, err := n.KV.Get("_sys:registry:active:legacy")
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Attempt string `codec:"a"`
	}
	if err := codec.NewDecoderBytes(active.Value, &codec.MsgpackHandle{}).Decode(&value); err != nil {
		t.Fatal(err)
	}
	if value.Attempt == "" || active.Epoch <= before.Epoch {
		t.Fatalf("promotion lost identity/fence: %q %+v", value.Attempt, active)
	}
}

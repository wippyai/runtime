// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"io"
	"testing"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/cluster/raft/multiplex"
)

// noopFSM is the multiplex primary slot for tests; it ignores everything.
type noopFSM struct{}

func (noopFSM) Apply(*hraft.Log) any                 { return nil }
func (noopFSM) Snapshot() (hraft.FSMSnapshot, error) { return noopSnap{}, nil }
func (noopFSM) Restore(rc io.ReadCloser) error       { return rc.Close() }

type noopSnap struct{}

func (noopSnap) Persist(sink hraft.SnapshotSink) error { return sink.Close() }
func (noopSnap) Release()                              {}

type clusterNode struct {
	raft  *hraft.Raft
	kvFSM *RaftFSM
}

func fastConfig(id string) *hraft.Config {
	c := hraft.DefaultConfig()
	c.LocalID = hraft.ServerID(id)
	c.HeartbeatTimeout = 50 * time.Millisecond
	c.ElectionTimeout = 50 * time.Millisecond
	c.LeaderLeaseTimeout = 50 * time.Millisecond
	c.CommitTimeout = 10 * time.Millisecond
	c.Logger = hclog.NewNullLogger()
	return c
}

// TestRaftCluster_KVReplicates wires three raw raft nodes — each with a
// multiplex router over a kv FSM — across an in-memory transport, then asserts a
// kv command applied on the leader replicates to every node's kv FSM.
func TestRaftCluster_KVReplicates(t *testing.T) {
	ids := []string{"n1", "n2", "n3"}
	nodes := make(map[string]*clusterNode, 3)
	transports := make(map[string]*hraft.InmemTransport, 3)

	for _, id := range ids {
		kvFSM := NewRaftFSM(nil)
		root := multiplex.New(noopFSM{}, kvFSM)
		store := hraft.NewInmemStore()
		snaps := hraft.NewInmemSnapshotStore()
		_, trans := hraft.NewInmemTransport(hraft.ServerAddress(id))
		r, err := hraft.NewRaft(fastConfig(id), root, store, store, snaps, trans)
		if err != nil {
			t.Fatalf("new raft %s: %v", id, err)
		}
		nodes[id] = &clusterNode{raft: r, kvFSM: kvFSM}
		transports[id] = trans
		t.Cleanup(func() { _ = r.Shutdown().Error() })
	}

	// Fully connect the in-memory transports.
	for a, ta := range transports {
		for b, tb := range transports {
			if a != b {
				ta.Connect(hraft.ServerAddress(b), tb)
			}
		}
	}

	// Bootstrap the 3-node cluster from n1.
	servers := make([]hraft.Server, 0, 3)
	for _, id := range ids {
		servers = append(servers, hraft.Server{
			Suffrage: hraft.Voter, ID: hraft.ServerID(id), Address: hraft.ServerAddress(id),
		})
	}
	if err := nodes["n1"].raft.BootstrapCluster(hraft.Configuration{Servers: servers}).Error(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	leader := waitLeader(t, nodes)
	t.Logf("leader = %s", leader)

	// Apply a kv Set on the leader, domain-tagged as the engine would.
	cmd := append([]byte{multiplex.KVDomain}, encodeCommand(command{Op: opSet, Key: "k", Value: []byte("v")})...)
	if err := nodes[leader].raft.Apply(cmd, 2*time.Second).Error(); err != nil {
		t.Fatalf("apply on leader: %v", err)
	}

	// Every node's kv FSM must converge on the value.
	deadline := time.Now().Add(3 * time.Second)
	for _, id := range ids {
		for {
			if e, ok := nodes[id].kvFSM.get("k"); ok && string(e.Value) == "v" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("node %s did not replicate k=v", id)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func waitLeader(t *testing.T, nodes map[string]*clusterNode) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for id, n := range nodes {
			if n.raft.State() == hraft.Leader {
				return id
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no leader elected")
	return ""
}

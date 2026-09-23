// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"io"
	"testing"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	hraft "github.com/hashicorp/raft"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/cluster/raft/multiplex"
	systemkv "github.com/wippyai/runtime/system/kv"
)

type realRaftNoopFSM struct{}

func (realRaftNoopFSM) Apply(*hraft.Log) any                 { return nil }
func (realRaftNoopFSM) Snapshot() (hraft.FSMSnapshot, error) { return realRaftNoopSnapshot{}, nil }
func (realRaftNoopFSM) Restore(rc io.ReadCloser) error       { return rc.Close() }

type realRaftNoopSnapshot struct{}

func (realRaftNoopSnapshot) Persist(sink hraft.SnapshotSink) error { return sink.Close() }
func (realRaftNoopSnapshot) Release()                              {}

// realRaftSubmitter forwards follower proposals to the elected leader. The
// test exercises the actual replicated KV FSM and Raft commit path while
// keeping the registry test independent of the production relay wiring.
type realRaftSubmitter struct {
	local *hraft.Raft
	peers map[string]*hraft.Raft
}

func (r *realRaftSubmitter) leader() *hraft.Raft {
	id, _ := r.local.LeaderWithID()
	return r.peers[string(id)]
}

func (r *realRaftSubmitter) Apply(cmd []byte, timeout time.Duration) (*raftapi.ApplyResponse, error) {
	target := r.local
	if target.State() != hraft.Leader {
		target = r.leader()
		if target == nil {
			return nil, raftapi.ErrNotLeader
		}
	}
	res := target.Apply(cmd, timeout)
	if err := res.Error(); err != nil {
		return nil, err
	}
	return &raftapi.ApplyResponse{Response: res.Response()}, nil
}

func (r *realRaftSubmitter) IsLeader() bool { return r.local.State() == hraft.Leader }

func (r *realRaftSubmitter) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	id, address := r.local.LeaderWithID()
	if id == "" {
		return "", "", raftapi.ErrNotLeader
	}
	return raftapi.ServerID(id), raftapi.ServerAddress(address), nil
}

func (r *realRaftSubmitter) Barrier(timeout time.Duration) error {
	return r.local.Barrier(timeout).Error()
}

func (r *realRaftSubmitter) CommitIndex() uint64 { return r.local.CommitIndex() }

func TestStrongPromotesAcrossThreeRealRaftNodes(t *testing.T) {
	ids := []string{"n1", "n2", "n3"}
	rafts := make(map[string]*hraft.Raft, len(ids))
	transports := make(map[string]*hraft.InmemTransport, len(ids))
	fsms := make(map[string]*systemkv.RaftFSM, len(ids))
	engines := make(map[string]*systemkv.RaftEngine, len(ids))
	registries := make(map[string]*Service, len(ids))

	for _, id := range ids {
		cfg := hraft.DefaultConfig()
		cfg.LocalID = hraft.ServerID(id)
		cfg.HeartbeatTimeout = 75 * time.Millisecond
		cfg.ElectionTimeout = 75 * time.Millisecond
		cfg.LeaderLeaseTimeout = 75 * time.Millisecond
		cfg.CommitTimeout = 15 * time.Millisecond
		cfg.Logger = hclog.NewNullLogger()
		fsm := systemkv.NewRaftFSM()
		fsms[id] = fsm
		root := multiplex.New(realRaftNoopFSM{}, fsm)
		store := hraft.NewInmemStore()
		snaps := hraft.NewInmemSnapshotStore()
		_, transport := hraft.NewInmemTransport(hraft.ServerAddress(id))
		r, err := hraft.NewRaft(cfg, root, store, store, snaps, transport)
		if err != nil {
			t.Fatalf("new raft %s: %v", id, err)
		}
		rafts[id] = r
		transports[id] = transport
		t.Cleanup(func() { _ = r.Shutdown().Error() })
	}
	for a, ta := range transports {
		for b, tb := range transports {
			if a != b {
				ta.Connect(hraft.ServerAddress(b), tb)
			}
		}
	}
	servers := make([]hraft.Server, 0, len(ids))
	for _, id := range ids {
		servers = append(servers, hraft.Server{Suffrage: hraft.Voter, ID: hraft.ServerID(id), Address: hraft.ServerAddress(id)})
	}
	if err := rafts[ids[0]].BootstrapCluster(hraft.Configuration{Servers: servers}).Error(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var leaderID string
	for time.Now().Before(deadline) {
		for _, id := range ids {
			if rafts[id].State() == hraft.Leader {
				leaderID = id
				break
			}
		}
		if leaderID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if leaderID == "" {
		t.Fatal("three-node raft did not elect a leader")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, id := range ids {
		peers := make(map[string]*hraft.Raft, len(rafts))
		for peerID, peer := range rafts {
			peers[peerID] = peer
		}
		submitter := &realRaftSubmitter{local: rafts[id], peers: peers}
		engine := systemkv.NewRaftEngine(submitter, fsms[id], id, nil, nil)
		if err := engine.Start(context.Background()); err != nil {
			t.Fatalf("start engine %s: %v", id, err)
		}
		engines[id] = engine
		t.Cleanup(func() { _ = engine.Stop() })
		registry := NewService(engine, id, nil, nil)
		registry.ConfigureStrong(StrongDeps{
			IsLeader:   func() bool { return rafts[id].State() == hraft.Leader },
			Deadline:   5 * time.Second,
		})
		registries[id] = registry
		if err := registry.StartReconciler(ctx); err != nil {
			t.Fatalf("start registry %s: %v", id, err)
		}
	}

	owner := pid.PID{Node: leaderID, Host: "h", UniqID: "real-raft"}
	out, err := registries[leaderID].RegisterScope(context.Background(), "real.raft", owner, global.Strong)
	if err != nil {
		t.Fatalf("strong register through three-node raft: %v", err)
	}
	if out.State != global.RegisterStateActive || out.PID.String() != owner.String() {
		t.Fatalf("strong outcome: %+v", out)
	}
	deadline = time.Now().Add(5 * time.Second)
	for _, id := range ids {
		for {
			if _, err := engines[id].Get(activeKey("real.raft")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("node %s did not apply promoted active record", id)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

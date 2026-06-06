// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	sysraft "github.com/wippyai/runtime/cluster/raft"
	"github.com/wippyai/runtime/cluster/raft/multiplex"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
	"go.uber.org/zap"
)

// Node is one member of the harness cluster.
type Node struct {
	ID      string
	Raft    *sysraft.Node
	KVFSM   *systemkv.RaftFSM
	KV      *systemkv.RaftEngine
	Primary *recordingFSM
	bus     *eventbus.Bus
	dataDir string
}

// Cluster is an in-process N-node raft+relay+kv cluster for end-to-end proof.
type Cluster struct {
	tb     testing.TB
	mesh   *mesh
	router *relayRouter
	base   string
	nodes  []*Node
	ids    []string
}

// NewCluster boots n nodes sharing one raft (multiplex primary + kv FSM), wires
// an in-process relay for kv leader-forwarding, gives each node a durable
// DataDir, bootstraps, and waits for a leader.
func NewCluster(tb testing.TB, n int) *Cluster {
	tb.Helper()
	c := &Cluster{
		tb:     tb,
		mesh:   newMesh(),
		router: newRelayRouter(),
		base:   tb.TempDir(),
	}
	c.router.mesh = c.mesh
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("node-%d", i)
		c.ids = append(c.ids, id)
	}
	for _, id := range c.ids {
		c.nodes = append(c.nodes, c.buildNode(id, filepath.Join(c.base, id)))
	}
	// Single-node bootstrap with the full server set: followers receive the
	// configuration via AppendEntries once the seed wins the election.
	if err := c.nodes[0].Raft.Bootstrap(c.ids); err != nil {
		tb.Fatalf("bootstrap: %v", err)
	}
	tb.Cleanup(c.Stop)
	c.WaitLeader(10 * time.Second)
	return c
}

func (c *Cluster) buildNode(id, dataDir string) *Node {
	bus := eventbus.NewBus()
	kvFSM := systemkv.NewRaftFSM(bus)
	primary := &recordingFSM{}
	root := multiplex.New(primary, kvFSM)

	cfg := raftapi.Config{
		HeartbeatTimeout: 600 * time.Millisecond,
		ElectionTimeout:  600 * time.Millisecond,
		CommitTimeout:    50 * time.Millisecond,
		DataDir:          filepath.Join(dataDir, "_sys", "raft"),
	}
	rn := sysraft.NewNode(id, root, cfg, bus, zap.NewNop(), nil, nil, nil)
	rn.SetConnectionManager(c.mesh.connect(id))

	kvEng := systemkv.NewRaftEngine(rn, kvFSM, bus, id, c.router, zap.NewNop())
	c.router.register(id, systemkv.KVRaftHostID, kvEng)

	statusCh, err := rn.Start(context.Background())
	if err != nil {
		c.tb.Fatalf("start raft %s: %v", id, err)
	}
	go drain(statusCh)
	if err := kvEng.Start(context.Background()); err != nil {
		c.tb.Fatalf("start kv %s: %v", id, err)
	}

	return &Node{ID: id, Raft: rn, KVFSM: kvFSM, KV: kvEng, Primary: primary, bus: bus, dataDir: dataDir}
}

func drain(ch <-chan any) {
	for range ch { //nolint:revive // draining
	}
}

// Nodes returns all nodes.
func (c *Cluster) Nodes() []*Node { return c.nodes }

// Node returns node i.
func (c *Cluster) Node(i int) *Node { return c.nodes[i] }

// Leader returns the current raft leader node, or nil.
func (c *Cluster) Leader() *Node {
	for _, n := range c.nodes {
		if n.Raft != nil && n.Raft.IsLeader() {
			return n
		}
	}
	return nil
}

// Follower returns any non-leader, non-killed node.
func (c *Cluster) Follower() *Node {
	for _, n := range c.nodes {
		if n.Raft != nil && !n.Raft.IsLeader() {
			return n
		}
	}
	return nil
}

// WaitLeader blocks until some node is leader.
func (c *Cluster) WaitLeader(timeout time.Duration) *Node {
	c.tb.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l := c.Leader(); l != nil {
			return l
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.tb.Fatalf("no leader within %s", timeout)
	return nil
}

// Partition isolates node i from the rest of the cluster (raft + relay) and
// clears raft peer failure state on both sides, as gossip NodeLeft would in prod.
func (c *Cluster) Partition(i int) {
	c.mesh.partition(c.ids[i])
	c.forgetSessions(i)
}

// Heal restores node i's links and clears stale raft peer failure state, matching
// the NodeLeft/rejoin path production uses for a fresh incarnation.
func (c *Cluster) Heal(i int) {
	c.mesh.heal(c.ids[i])
	c.forgetSessions(i)
}

// forgetSessions clears raft peer state between node i and every other node on
// both sides. Kept as a helper name for older tests.
func (c *Cluster) forgetSessions(i int) {
	id := c.ids[i]
	for j, n := range c.nodes {
		if j == i || n.Raft == nil {
			continue
		}
		n.Raft.OnNodeLeft(id)
		if c.nodes[i].Raft != nil {
			c.nodes[i].Raft.OnNodeLeft(c.ids[j])
		}
	}
}

// Kill stops node i's raft and marks it unreachable (hard crash).
func (c *Cluster) Kill(i int) {
	n := c.nodes[i]
	c.mesh.setDown(n.ID, true)
	if n.KV != nil {
		_ = n.KV.Stop()
	}
	if n.Raft != nil {
		_ = n.Raft.Stop(context.Background())
	}
	c.forgetSessions(i)
}

// Restart rebuilds node i from its durable DataDir (no re-bootstrap; it recovers
// from disk and rejoins), proving durability.
func (c *Cluster) Restart(i int) {
	id := c.ids[i]
	c.router.unregister(id)
	c.mesh.setDown(id, false)
	c.nodes[i] = c.buildNode(id, filepath.Join(c.base, id))
	// Peers still hold sessions to the dead incarnation; force a rebuild to the
	// fresh node.
	c.forgetSessions(i)
}

// Stop tears down all nodes.
func (c *Cluster) Stop() {
	for _, n := range c.nodes {
		if n.KV != nil {
			_ = n.KV.Stop()
		}
		if n.Raft != nil {
			_ = n.Raft.Stop(context.Background())
		}
		if n.bus != nil {
			n.bus.Stop()
		}
	}
	c.mesh.stopAll()
}

// --- in-process relay router ---

type relayRouter struct {
	hosts map[cluster.NodeID]map[pid.HostID]relay.Receiver
	mesh  *mesh
	mu    sync.Mutex
}

func newRelayRouter() *relayRouter {
	return &relayRouter{hosts: map[cluster.NodeID]map[pid.HostID]relay.Receiver{}}
}

func (r *relayRouter) register(node cluster.NodeID, host pid.HostID, recv relay.Receiver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hosts[node] == nil {
		r.hosts[node] = map[pid.HostID]relay.Receiver{}
	}
	r.hosts[node][host] = recv
}

func (r *relayRouter) unregister(node cluster.NodeID) {
	r.mu.Lock()
	delete(r.hosts, node)
	r.mu.Unlock()
}

// Send routes a relay package to the target node's host receiver, honoring mesh
// partitions/down state.
func (r *relayRouter) Send(pkg *relay.Package) error {
	src := pkg.Source.Node
	dst := pkg.Target.Node
	if r.mesh != nil && !r.mesh.reachable(src, dst) {
		relay.ReleasePackage(pkg)
		return errBlocked
	}
	r.mu.Lock()
	hosts := r.hosts[dst]
	r.mu.Unlock()
	if hosts == nil {
		relay.ReleasePackage(pkg)
		return fmt.Errorf("clustertest: no node %q", dst)
	}
	recv := hosts[pkg.Target.Host]
	if recv == nil {
		relay.ReleasePackage(pkg)
		return fmt.Errorf("clustertest: no host %q on %q", pkg.Target.Host, dst)
	}
	return recv.Send(pkg)
}

var _ relay.Receiver = (*relayRouter)(nil)

// recordingFSM is the multiplex primary slot: it records applied untagged
// command payloads so a test can prove the shared raft carries non-kv commands
// alongside kv ones.
type recordingFSM struct {
	applied [][]byte
	mu      sync.Mutex
}

func (f *recordingFSM) Apply(log *hraft.Log) any {
	f.mu.Lock()
	cp := make([]byte, len(log.Data))
	copy(cp, log.Data)
	f.applied = append(f.applied, cp)
	f.mu.Unlock()
	return nil
}

func (f *recordingFSM) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied)
}

func (f *recordingFSM) Snapshot() (hraft.FSMSnapshot, error) { return recSnap{}, nil }
func (f *recordingFSM) Restore(rc io.ReadCloser) error       { return rc.Close() }

type recSnap struct{}

func (recSnap) Persist(s hraft.SnapshotSink) error { return s.Close() }
func (recSnap) Release()                           {}

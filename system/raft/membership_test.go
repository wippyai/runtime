// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// fakeRaft is a minimal raftapi.Service stub that records ops.
type fakeRaft struct {
	// Configurable per-method failure injection.
	addVoterErr    error
	addNonvoterErr error
	demoteErr      error
	removeErr      error
	transferErr    error

	servers []raftapi.Server
	ops     []recordedOp

	mu                 sync.Mutex
	transferCalled     int
	leader             bool
	transferTookLeader bool
}

type recordedOp struct {
	Kind string
	ID   string
	Addr string
}

func newFakeRaft(leader bool, servers []raftapi.Server) *fakeRaft {
	return &fakeRaft{leader: leader, servers: servers}
}

func (f *fakeRaft) IsLeader() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.leader
}

func (f *fakeRaft) GetConfiguration() ([]raftapi.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]raftapi.Server, len(f.servers))
	copy(out, f.servers)
	return out, nil
}

func (f *fakeRaft) AddVoter(id raftapi.ServerID, addr raftapi.ServerAddress, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addVoterErr != nil {
		return f.addVoterErr
	}
	f.ops = append(f.ops, recordedOp{Kind: "AddVoter", ID: id, Addr: addr})
	// Apply to in-memory config so subsequent GetConfiguration reflects it.
	f.upsertLocked(raftapi.Server{ID: id, Address: addr, IsVoter: true})
	return nil
}

func (f *fakeRaft) AddNonvoter(id raftapi.ServerID, addr raftapi.ServerAddress, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addNonvoterErr != nil {
		return f.addNonvoterErr
	}
	f.ops = append(f.ops, recordedOp{Kind: "AddNonvoter", ID: id, Addr: addr})
	f.upsertLocked(raftapi.Server{ID: id, Address: addr, IsVoter: false})
	return nil
}

func (f *fakeRaft) DemoteVoter(id raftapi.ServerID, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.demoteErr != nil {
		return f.demoteErr
	}
	f.ops = append(f.ops, recordedOp{Kind: "DemoteVoter", ID: id})
	for i, s := range f.servers {
		if s.ID == id {
			f.servers[i].IsVoter = false
		}
	}
	return nil
}

func (f *fakeRaft) RemoveServer(id raftapi.ServerID, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	f.ops = append(f.ops, recordedOp{Kind: "RemoveServer", ID: id})
	out := f.servers[:0]
	for _, s := range f.servers {
		if s.ID != id {
			out = append(out, s)
		}
	}
	f.servers = out
	return nil
}

func (f *fakeRaft) LeadershipTransfer(_ raftapi.ServerID, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transferCalled++
	if f.transferErr != nil {
		return f.transferErr
	}
	if f.transferTookLeader {
		f.leader = false
	}
	return nil
}

// Unused interface methods.
func (f *fakeRaft) Apply(_ []byte, _ time.Duration) (*raftapi.ApplyResponse, error) {
	return nil, nil
}
func (f *fakeRaft) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) { return "", "", nil }
func (f *fakeRaft) LeaderCh() <-chan bool                                    { return nil }
func (f *fakeRaft) State() raftapi.State                                     { return raftapi.Follower }
func (f *fakeRaft) Barrier(_ time.Duration) error                            { return nil }

func (f *fakeRaft) upsertLocked(s raftapi.Server) {
	for i, existing := range f.servers {
		if existing.ID == s.ID {
			f.servers[i] = s
			return
		}
	}
	f.servers = append(f.servers, s)
}

func (f *fakeRaft) recordedOps() []recordedOp {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedOp, len(f.ops))
	copy(out, f.ops)
	return out
}

// fakeMembership satisfies cluster.Membership.
type fakeMembership struct {
	local cluster.NodeInfo
	nodes []cluster.NodeInfo
	mu    sync.Mutex
}

func (m *fakeMembership) Nodes() []cluster.NodeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]cluster.NodeInfo, len(m.nodes))
	copy(out, m.nodes)
	return out
}

func (m *fakeMembership) LocalNode() cluster.NodeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.local
}

func mkNode(id, addr, raftPort string) cluster.NodeInfo {
	return cluster.NodeInfo{
		ID:   id,
		Addr: addr,
		Meta: cluster.NodeMeta{"raft_port": raftPort},
	}
}

// reconcile drives one pass synchronously without timing dependencies.
// Returns a fresh handler so each test is isolated.
func newTestHandler(t *testing.T, fr *fakeRaft, fm *fakeMembership) *MembershipHandler {
	t.Helper()
	h := NewMembershipHandler(fr, fm, nil, HandlerConfig{
		MaxVoters:         5,
		ReconcileDebounce: 10 * time.Millisecond,
		ReconcileTimeout:  500 * time.Millisecond,
	}, zaptest.NewLogger(t))
	return h
}

func TestRunReconcileOnce_NonLeaderNoOps(t *testing.T) {
	fr := newFakeRaft(false, nil)
	fm := &fakeMembership{nodes: []cluster.NodeInfo{mkNode("a", "10.0.0.1", "7960")}}
	h := newTestHandler(t, fr, fm)
	h.runReconcileOnce(context.Background())
	assert.Empty(t, fr.recordedOps(), "non-leader must not mutate raft config")
}

func TestRunReconcileOnce_BootstrapAdds(t *testing.T) {
	fr := newFakeRaft(true, []raftapi.Server{
		{ID: "a", Address: "a", IsVoter: true}, // bootstrapped self
	})
	fm := &fakeMembership{
		local: mkNode("a", "10.0.0.1", "7960"),
		nodes: []cluster.NodeInfo{
			mkNode("a", "10.0.0.1", "7960"),
			mkNode("b", "10.0.0.2", "7960"),
			mkNode("c", "10.0.0.3", "7960"),
		},
	}
	h := newTestHandler(t, fr, fm)
	h.runReconcileOnce(context.Background())

	ops := fr.recordedOps()
	require.Len(t, ops, 2)
	for _, op := range ops {
		assert.Equal(t, "AddVoter", op.Kind)
	}
}

func TestRunReconcileOnce_VoterCapDemotesSurplus(t *testing.T) {
	// 7 nodes, cap=5 → 5 voters, 2 nonvoters.
	servers := []raftapi.Server{}
	nodes := []cluster.NodeInfo{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		servers = append(servers, raftapi.Server{
			ID: id, Address: id, IsVoter: true,
		})
		nodes = append(nodes, mkNode(id, "10.0.0."+id, "7960"))
	}
	fr := newFakeRaft(true, servers)
	fm := &fakeMembership{local: nodes[0], nodes: nodes}
	h := newTestHandler(t, fr, fm)
	h.runReconcileOnce(context.Background())

	// Two demotions expected (f, g — alphabetically last).
	demoted := 0
	for _, op := range fr.recordedOps() {
		if op.Kind == "DemoteVoter" {
			demoted++
		}
	}
	assert.Equal(t, 2, demoted, "2 surplus voters must be demoted")
}

func TestRunReconcileOnce_NoOpAtSteadyState(t *testing.T) {
	servers := []raftapi.Server{
		{ID: "a", Address: "a", IsVoter: true},
		{ID: "b", Address: "b", IsVoter: true},
		{ID: "c", Address: "c", IsVoter: true},
	}
	nodes := []cluster.NodeInfo{
		mkNode("a", "10.0.0.1", "7960"),
		mkNode("b", "10.0.0.2", "7960"),
		mkNode("c", "10.0.0.3", "7960"),
	}
	fr := newFakeRaft(true, servers)
	fm := &fakeMembership{local: nodes[0], nodes: nodes}
	h := newTestHandler(t, fr, fm)
	h.runReconcileOnce(context.Background())
	assert.Empty(t, fr.recordedOps(), "steady-state cluster must produce no ops")
}

// TestRunReconcileOnce_NodeLeftRemovesServer reproduces the reviewer's
// requested scenario from PR #241 issue 4494041044 test item 8:
// "mesh-backed Raft transport under node leave/rejoin". The chaos
// rig run_raft_over_mesh.sh covers the integration-level cycle; this
// unit-level companion asserts the membership handler's reconcile
// path observes a vanished peer (no longer in the membership
// snapshot) and emits exactly one RemoveServer op against the raft
// configuration. Because RemoveServer is the only mutation the
// handler issues here, this also pins the contract that internode
// teardown of the departed peer is not initiated by the membership
// handler — it is the connection manager's own NodeLeft observer
// that owns that step (see cluster/internode/manager.go).
func TestRunReconcileOnce_NodeLeftRemovesServer(t *testing.T) {
	fr := newFakeRaft(true, []raftapi.Server{
		{ID: "a", Address: "a", IsVoter: true},
		{ID: "b", Address: "b", IsVoter: true},
		{ID: "c", Address: "c", IsVoter: true},
	})
	fm := &fakeMembership{
		local: mkNode("a", "10.0.0.1", "7960"),
		nodes: []cluster.NodeInfo{
			mkNode("a", "10.0.0.1", "7960"),
			mkNode("b", "10.0.0.2", "7960"),
		},
	}
	h := newTestHandler(t, fr, fm)
	h.runReconcileOnce(context.Background())

	ops := fr.recordedOps()
	var removes []recordedOp
	for _, op := range ops {
		if op.Kind == "RemoveServer" {
			removes = append(removes, op)
		}
	}
	require.Len(t, removes, 1, "exactly one RemoveServer op expected; got ops=%+v", ops)
	assert.Equal(t, "c", removes[0].ID, "the departed peer must be the one removed")
}

func TestRunReconcileOnce_LocalLeaderSelfRemovalTransfersFirst(t *testing.T) {
	// Local node "a" is leader and has explicitly opted out of raft
	// (raft_eligible=false). Reconcile must transfer leadership before
	// attempting self-remove. Note: simply being absent from
	// Membership.Nodes() is no longer sufficient — the reconciler always
	// includes the local node as a candidate so a healthy leader is never
	// evicted by accident.
	fr := newFakeRaft(true, []raftapi.Server{
		{ID: "a", Address: "a", IsVoter: true},
		{ID: "b", Address: "b", IsVoter: true},
		{ID: "c", Address: "c", IsVoter: true},
	})
	fr.transferTookLeader = true
	localOptedOut := mkNode("a", "10.0.0.1", "7960")
	localOptedOut.Meta["raft_eligible"] = "false"
	fm := &fakeMembership{
		local: localOptedOut,
		nodes: []cluster.NodeInfo{
			mkNode("b", "10.0.0.2", "7960"),
			mkNode("c", "10.0.0.3", "7960"),
		},
	}
	h := newTestHandler(t, fr, fm)
	h.runReconcileOnce(context.Background())

	assert.Equal(t, 1, fr.transferCalled, "self-removal must transfer leadership first")
	// RemoveServer for "a" must NOT have run on this pass — we returned
	// ErrLeadershipLost (soft error) and aborted.
	for _, op := range fr.recordedOps() {
		if op.Kind == "RemoveServer" && op.ID == "a" {
			t.Fatal("self-remove should not run after leadership transfer in same pass")
		}
	}
}

func TestRunReconcileOnce_SoftErrorAbortsPass(t *testing.T) {
	fr := newFakeRaft(true, []raftapi.Server{
		{ID: "a", Address: "a", IsVoter: true},
	})
	fr.addVoterErr = raftapi.ErrLeadershipLost
	fm := &fakeMembership{
		local: mkNode("a", "10.0.0.1", "7960"),
		nodes: []cluster.NodeInfo{
			mkNode("a", "10.0.0.1", "7960"),
			mkNode("b", "10.0.0.2", "7960"),
			mkNode("c", "10.0.0.3", "7960"),
		},
	}
	h := newTestHandler(t, fr, fm)
	h.runReconcileOnce(context.Background())

	// No further ops should have been recorded after the soft error fired
	// on the first AddVoter — pass aborts.
	assert.Empty(t, fr.recordedOps())
}

func TestSubscriberLoop_DebouncesAndCoalesces(t *testing.T) {
	// End-to-end: bus event → debounce window → one reconcile pass per burst.
	bus := eventbus.NewBus()
	defer bus.Stop()

	fr := newFakeRaft(true, []raftapi.Server{
		{ID: "a", Address: "a", IsVoter: true},
	})
	fm := &fakeMembership{
		local: mkNode("a", "10.0.0.1", "7960"),
		nodes: []cluster.NodeInfo{mkNode("a", "10.0.0.1", "7960")},
	}
	h := NewMembershipHandler(fr, fm, bus, HandlerConfig{
		MaxVoters:         5,
		ReconcileDebounce: 50 * time.Millisecond,
		ReconcileTimeout:  500 * time.Millisecond,
	}, zaptest.NewLogger(t))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, h.Start(ctx))
	defer h.Stop()

	// Fire 5 events in quick succession — should produce at most 1 reconcile pass.
	for i := 0; i < 5; i++ {
		bus.Send(ctx, event.Event{
			System: cluster.System,
			Kind:   cluster.NodeJoined,
			Data:   cluster.NodeEvent{Node: mkNode("b", "10.0.0.2", "7960")},
		})
	}

	// Wait for debounce + slack.
	time.Sleep(150 * time.Millisecond)

	// Add membership entries so reconcile has something to do. Use 3 total
	// (a + b + c) so desiredVoterCount returns 3 and b becomes a voter
	// (rather than nonvoter under the even-count clamp).
	fm.mu.Lock()
	fm.nodes = append(fm.nodes,
		mkNode("b", "10.0.0.2", "7960"),
		mkNode("c", "10.0.0.3", "7960"),
	)
	fm.mu.Unlock()

	// One more event after we updated membership.
	bus.Send(ctx, event.Event{
		System: cluster.System,
		Kind:   cluster.NodeJoined,
		Data:   cluster.NodeEvent{Node: mkNode("b", "10.0.0.2", "7960")},
	})
	time.Sleep(150 * time.Millisecond)

	ops := fr.recordedOps()
	addVoters := 0
	for _, op := range ops {
		if op.Kind == "AddVoter" && op.ID == "b" {
			addVoters++
		}
	}
	assert.Equal(t, 1, addVoters, "debounced burst should produce exactly one AddVoter for b")
}

// quietLogger silences zap output for tests that exercise warning paths
// where output would clutter results.
func quietLogger() *zap.Logger { return zap.NewNop() }

func TestHandlerConfig_ApplyDefaults_RoundsEvenDown(t *testing.T) {
	cfg := HandlerConfig{MaxVoters: 6}.applyDefaults(quietLogger())
	assert.Equal(t, 5, cfg.MaxVoters)
}

func TestHandlerConfig_ApplyDefaults_FillsZero(t *testing.T) {
	cfg := HandlerConfig{}.applyDefaults(quietLogger())
	assert.Equal(t, defaultMaxVoters, cfg.MaxVoters)
	assert.Equal(t, defaultReconcileDebounce, cfg.ReconcileDebounce)
	assert.Equal(t, defaultReconcileTimeout, cfg.ReconcileTimeout)
}

func TestHandlerConfig_ApplyDefaults_ClampsToOne(t *testing.T) {
	// MaxVoters=2 → rounds down to 1 (not 0).
	cfg := HandlerConfig{MaxVoters: 2}.applyDefaults(quietLogger())
	assert.Equal(t, 1, cfg.MaxVoters)
}

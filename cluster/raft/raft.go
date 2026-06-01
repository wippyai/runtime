// SPDX-License-Identifier: MPL-2.0

// Package raft provides a Raft consensus node integrated with the wippy cluster.
package raft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	hraft "github.com/hashicorp/raft"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/cluster/internode"
)

// Node wraps a hashicorp/raft instance and integrates it with the wippy
// event bus and cluster membership system.
type Node struct {
	fsm         hraft.FSM
	bus         event.Bus
	logStore    hraft.LogStore
	stableStore hraft.StableStore
	snapStore   hraft.SnapshotStore
	transport   hraft.Transport
	streamLayer *meshStreamLayer
	connMgr     internode.ConnectionManager
	logger      *zap.Logger
	raft        *hraft.Raft
	stopCh      chan struct{}
	tel         *telemetry
	localID     string
	config      raftapi.Config
	voterCap    int
	mu          sync.Mutex
	started     bool
}

// NewNode creates a new Raft node. The FSM must be provided by the caller
// (e.g., the global registry state machine).
func NewNode(localID string, fsm hraft.FSM, cfg raftapi.Config, bus event.Bus, logger *zap.Logger,
	coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) *Node {
	cfg.InitDefaults()
	return &Node{
		fsm:     fsm,
		config:  cfg,
		localID: localID,
		bus:     bus,
		logger:  logger,
		stopCh:  make(chan struct{}),
		tel:     newTelemetry(coll, mp, tp),
	}
}

// LocalID returns the NodeID this raft instance was constructed with.
// Used by the bootstrap watcher and other coordinator goroutines that
// need to reason about the local node without consulting the transport.
func (n *Node) LocalID() string { return n.localID }

// SetConnectionManager wires the wippy internode connection manager
// that the mesh-backed Raft transport rides on top of. Must be called
// before Start; calling Start without a connection manager set returns
// an error. Idempotent before Start; ignored after.
func (n *Node) SetConnectionManager(connMgr internode.ConnectionManager) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.started {
		return
	}
	n.connMgr = connMgr
}

// OnNodeLeft tears down the mesh transport session for a departed node
// AND clears any accumulated per-peer failure state on the peer-state
// tracker. Wired by boot to cluster.NodeLeft so a node's per-peer yamux
// session, its backing classConn, and the session acceptLoop goroutine
// are released on departure instead of leaking, and so a rejoin builds
// a fresh session rather than reusing the stale one. Clearing the
// tracker state means a reborn pod (same NodeID, fresh process) is not
// stuck behind the exponential dead-window backoff that grew while its
// previous incarnation was failing. No-op before Start (no stream layer
// or transport wired yet).
func (n *Node) OnNodeLeft(node cluster.NodeID) {
	n.mu.Lock()
	sl := n.streamLayer
	tracker, _ := n.transport.(*peerStateTracker)
	n.mu.Unlock()
	if node == "" || node == n.localID {
		return
	}
	if sl != nil {
		sl.removePeer(node)
	}
	if tracker != nil {
		tracker.forgetPeer(hraft.ServerAddress(node))
	}
}

// Telemetry exposes the internal telemetry handle so the MembershipHandler
// can emit voter-op metrics without an extra constructor argument.
// Stable enough for in-package use; not part of raftapi.Service.
func (n *Node) Telemetry() *telemetry { return n.tel }

// PeerDeadStreak returns the current per-peer "consecutive dead-window
// trips" count from the peerStateTracker. Returns 0 if no tracker is wired
// or the peer is unknown. Used by the MembershipHandler to drive proactive
// voter eviction when transport heartbeats AND gossip both say a peer is
// down. In-package only.
func (n *Node) PeerDeadStreak(addr raftapi.ServerAddress) int {
	tracker, ok := n.transport.(*peerStateTracker)
	if !ok {
		return 0
	}
	return tracker.DeadStreak(hraft.ServerAddress(addr))
}

// SetVoterCap records the configured voter ceiling so the voter-ladder
// telemetry can publish it alongside the live counts. Safe to call before
// Start; the membership handler invokes this with HandlerConfig.MaxVoters.
func (n *Node) SetVoterCap(maxVoters int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.voterCap = maxVoters
}

// sampleVoterLadder reads the current Raft configuration and emits the
// voter / non-voter / voter-cap gauges. Best-effort: a failure to read
// configuration is logged once at debug and otherwise silently dropped so
// telemetry never blocks a control-plane op.
func (n *Node) sampleVoterLadder() {
	if n.raft == nil {
		return
	}

	f := n.raft.GetConfiguration()
	if err := f.Error(); err != nil {
		n.logger.Debug("voter ladder: GetConfiguration failed", zap.Error(err))
		return
	}

	voters, nonVoters := 0, 0
	for _, s := range f.Configuration().Servers {
		if s.Suffrage == hraft.Voter {
			voters++
		} else {
			nonVoters++
		}
	}

	n.mu.Lock()
	voterCap := n.voterCap
	n.mu.Unlock()

	n.tel.recordVoterLadder(voters, nonVoters, voterCap)
}

// Start initializes storage, transport, and the Raft instance.
// If Bootstrap is true and no existing state is found, it bootstraps a
// single-node cluster.
func (n *Node) Start(_ context.Context) (<-chan any, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.started {
		return nil, fmt.Errorf("raft node already started")
	}

	// Raft rides the wippy internode mesh exclusively; the connection
	// manager must be wired via SetConnectionManager before Start.
	if n.connMgr == nil {
		return nil, fmt.Errorf("raft: connection manager not set (call SetConnectionManager before Start)")
	}

	// Diskless control plane: the cluster state is ephemeral; on restart a
	// node rejoins quorum and replays from peers. Bounding raft to in-memory
	// stores matches the design (Erlang-global / Akka-ddata style) and
	// removes the persistence-vs-quorum failure modes that disk introduces.
	n.logStore = hraft.NewInmemStore()
	n.stableStore = hraft.NewInmemStore()
	n.snapStore = hraft.NewInmemSnapshotStore()

	// Pipe hashicorp/raft's transport-internal logger through zap with
	// per-line rate limiting. Default behavior writes broken-pipe storms
	// straight to os.Stderr, which under network-partition chaos produces
	// thousands of unsampled lines per second per pod.
	netLogOut := newRaftStderrAdapter(n.logger.Named("raft-net"))

	// Mesh-backed transport: yamux session per peer over the existing
	// internode connection (ClassRaftMesh frames). No dedicated Raft
	// listener is bound; peers are addressed by NodeID.
	n.streamLayer = newMeshStreamLayer(n.localID, n.connMgr, n.logger.Named("raft-mesh"))
	if err := n.streamLayer.register(); err != nil {
		return nil, fmt.Errorf("register raft mesh receiver: %w", err)
	}
	var inner hraft.Transport = hraft.NewNetworkTransport(n.streamLayer, n.config.MaxPool, 10*time.Second, netLogOut)

	// Stack: peerStateTracker over instrumentedTransport over the
	// chosen inner transport. The tracker short-circuits writes to
	// peers that have produced N consecutive errors, breaking the
	// broken-pipe storm we see under chaos partition without changing
	// raft semantics.
	n.transport = newPeerStateTracker(
		&instrumentedTransport{Transport: inner, tel: n.tel},
		n.tel,
	)

	// Create Raft instance. The FSM is wrapped so Snapshot/Persist calls
	// emit OTel metrics and spans without leaking knowledge of telemetry
	// into FSM implementations.
	rc := toHashicorpConfig(n.localID, n.config)
	wrappedFSM := &instrumentedFSM{FSM: n.fsm, tel: n.tel}
	r, err := hraft.NewRaft(rc, wrappedFSM, n.logStore, n.stableStore, n.snapStore, n.transport)
	if err != nil {
		if closer, ok := inner.(hraft.WithClose); ok {
			_ = closer.Close()
		}
		return nil, fmt.Errorf("create raft instance: %w", err)
	}
	n.raft = r

	// Cluster formation is deferred to the gossip-driven bootstrap watcher
	// (see bootstrap.go). The watcher observes the converged gossip view
	// and calls Bootstrap once exactly BootstrapExpect raft-eligible peers
	// are visible. Start does not block on bootstrap; nodes joining an
	// already-formed cluster never bootstrap and are added by the leader's
	// reconciler via AddVoter.

	n.started = true

	// Start leadership monitor goroutine.
	statusCh := make(chan any, 4)
	go n.monitorLeadership(statusCh)

	n.logger.Info("raft node started",
		zap.String("id", n.localID),
		zap.String("transport", "mesh"),
		zap.Int("bootstrap_expect", n.config.BootstrapExpect))

	return statusCh, nil
}

// Stop gracefully shuts down the Raft node.
func (n *Node) Stop(_ context.Context) error {
	n.mu.Lock()
	if !n.started {
		n.mu.Unlock()
		return nil
	}
	n.started = false
	n.mu.Unlock()

	close(n.stopCh)

	// Attempt graceful leadership transfer before shutdown.
	if n.raft.State() == hraft.Leader {
		n.logger.Info("transferring raft leadership before shutdown")
		if err := n.raft.LeadershipTransfer().Error(); err != nil {
			n.logger.Warn("leadership transfer failed", zap.Error(err))
		}
	}

	f := n.raft.Shutdown()
	if err := f.Error(); err != nil {
		return fmt.Errorf("raft shutdown: %w", err)
	}

	if n.transport != nil {
		if closer, ok := n.transport.(hraft.WithClose); ok {
			if err := closer.Close(); err != nil {
				n.logger.Warn("raft transport close failed", zap.Error(err))
			}
		}
	}

	n.logger.Info("raft node stopped", zap.String("id", n.localID))
	return nil
}

// monitorLeadership watches the Raft leadership channel and publishes
// events to the event bus. It also samples raft state/term on a 1s ticker
// so telemetry stays fresh even when LeaderCh is quiet.
func (n *Node) monitorLeadership(statusCh chan<- any) {
	defer close(statusCh)

	leaderCh := n.raft.LeaderCh()

	sampleTicker := time.NewTicker(time.Second)
	defer sampleTicker.Stop()

	// electionStart is set whenever we leave leader state, to compute
	// election duration when we (re)enter it. wasLeader is the last
	// observed leadership state. Both are touched only by this goroutine,
	// so applyTransition needs no locking.
	var electionStart time.Time
	wasLeader := false

	// applyTransition fires the elected/lost side effects (election timing,
	// leader-change counter, log line, bus event) when observed leadership
	// diverges from wasLeader, and updates the tracking vars. reason tags
	// the log line to distinguish the seed / LeaderCh / reconcile paths.
	applyTransition := func(nowLeader bool, reason string) {
		switch {
		case nowLeader && !wasLeader:
			if !electionStart.IsZero() {
				n.tel.recordElection(time.Since(electionStart))
			}
			n.tel.recordLeaderChange()
			wasLeader = true
			n.logger.Info("this node is now the raft leader"+reason, zap.String("id", n.localID))
			n.bus.Send(context.Background(), event.Event{
				System: cluster.System,
				Kind:   cluster.LeaderElected,
				Path:   n.localID,
			})
		case !nowLeader && wasLeader:
			wasLeader = false
			electionStart = time.Now()
			n.logger.Info("this node lost raft leadership"+reason, zap.String("id", n.localID))
			n.bus.Send(context.Background(), event.Event{
				System: cluster.System,
				Kind:   cluster.LeaderLost,
				Path:   n.localID,
			})
		}
	}

	// Seed the initial state: hashicorp/raft's LeaderCh is non-buffered with
	// non-blocking writes, so the initial `true` fired during BootstrapCluster
	// (before this goroutine reads) is dropped. Without seeding from the
	// current state, raft_leader_changes_total stays 0 for a node that became
	// leader at startup.
	applyTransition(n.raft.State() == hraft.Leader, " (initial state)")

	// Initial sample so dashboards see state immediately.
	n.sampleStateAndTerm()
	n.sampleVoterLadder()

	for {
		select {
		case isLeader, ok := <-leaderCh:
			if !ok {
				return
			}
			applyTransition(isLeader, "")
			n.sampleStateAndTerm()
			n.sampleVoterLadder()
		case <-sampleTicker.C:
			// Defense-in-depth reconciliation: LeaderCh transitions that fire
			// while the goroutine is between selects are dropped silently, so
			// compare actual state vs wasLeader each tick and fire any missed
			// transition. Without this the first leader-elected after
			// BootstrapCluster is frequently lost.
			applyTransition(n.raft.State() == hraft.Leader, " (reconciled)")
			n.sampleStateAndTerm()
			n.sampleVoterLadder()
		case <-n.stopCh:
			return
		}
	}
}

// sampleStateAndTerm reads current raft state/term/log indices and emits gauge
// samples. Safe to call concurrently with raft operations; hraft.Stats() is
// goroutine-safe.
func (n *Node) sampleStateAndTerm() {
	if n.raft == nil {
		return
	}

	n.tel.recordState(n.localID, strings.ToLower(n.raft.State().String()))

	stats := n.raft.Stats()
	if v, err := strconv.ParseUint(stats["term"], 10, 64); err == nil {
		n.tel.recordTerm(v)
	}

	var commit uint64
	commitOK := false
	if v, err := strconv.ParseUint(stats["commit_index"], 10, 64); err == nil {
		n.tel.recordCommitIndex(v)
		commit = v
		commitOK = true
	}
	if v, err := strconv.ParseUint(stats["last_log_index"], 10, 64); err == nil {
		n.tel.recordLastLogIndex(n.localID, v)
		if commitOK {
			n.tel.recordLogLag(n.localID, int64(commit)-int64(v))
		}
	}
}

// --- raft.Service interface implementation ---

// Apply proposes a command to the Raft log.
func (n *Node) Apply(cmd []byte, timeout time.Duration) (*raftapi.ApplyResponse, error) {
	if n.raft == nil {
		return nil, raftapi.ErrNotRunning
	}
	f := n.raft.Apply(cmd, timeout)
	if err := f.Error(); err != nil {
		return nil, n.translateError(err)
	}
	return &raftapi.ApplyResponse{
		Response: f.Response(),
		Index:    f.Index(),
	}, nil
}

// Leader returns the current leader.
func (n *Node) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	if n.raft == nil {
		return "", "", raftapi.ErrNotRunning
	}
	addr, id := n.raft.LeaderWithID()
	if addr == "" {
		return "", "", raftapi.ErrNoLeader
	}
	return string(id), string(addr), nil
}

// IsLeader returns true if this node is the leader.
func (n *Node) IsLeader() bool {
	if n.raft == nil {
		return false
	}
	return n.raft.State() == hraft.Leader
}

// LeaderCh returns the leadership notification channel.
func (n *Node) LeaderCh() <-chan bool {
	if n.raft == nil {
		ch := make(chan bool)
		close(ch)
		return ch
	}
	return n.raft.LeaderCh()
}

// State returns the current Raft state.
func (n *Node) State() raftapi.State {
	if n.raft == nil {
		return raftapi.Shutdown
	}
	switch n.raft.State() {
	case hraft.Follower:
		return raftapi.Follower
	case hraft.Candidate:
		return raftapi.Candidate
	case hraft.Leader:
		return raftapi.Leader
	default:
		return raftapi.Shutdown
	}
}

// CommitIndex returns the highest committed log index. Used by callers
// that need to filter "log committed" from "log present" — e.g.
// LogHead's cross-pod comparison, which depends on Raft's Log Matching
// Property and therefore must skip the (legitimately divergent)
// uncommitted suffix. Sourced from hraft.Stats()["commit_index"], which
// is the same map sampleStateAndTerm reads at 1Hz.
func (n *Node) CommitIndex() uint64 {
	if n.raft == nil {
		return 0
	}
	v, err := strconv.ParseUint(n.raft.Stats()["commit_index"], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// Term returns the node's current Raft term. Used by chaos invariants
// that need to distinguish a stale leader (pre-step-down, older term)
// from a real split-brain (two leaders at the same term). Sourced from
// hraft.Stats()["term"].
func (n *Node) Term() uint64 {
	if n.raft == nil {
		return 0
	}
	v, err := strconv.ParseUint(n.raft.Stats()["term"], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// LastContact reports the time of last contact with the current Raft
// leader. Used by the runtime liveness probe to detect a follower that
// has lost contact with the leader (partition isolation). Leaders
// always return zero time.Time, which the caller must special-case.
func (n *Node) LastContact() time.Time {
	if n.raft == nil {
		return time.Time{}
	}
	return n.raft.LastContact()
}

// IsVoter reports whether the local node currently appears as a voter
// in the cluster configuration. Returns false when the configuration
// can't be read (e.g. before Start, or under transport failures); the
// caller is expected to treat that as "not a voter" for purposes that
// depend on quorum participation.
//
// Used by the runtime liveness probe to apply a stricter staleness
// ceiling to voters (whose heartbeat lag matters for quorum) than to
// non-voters (replication-only learners that lag is naturally larger
// for at scale, since the leader fans heartbeats out to every follower
// each tick — at 60+ followers this fan-out alone bounds the rate).
func (n *Node) IsVoter() bool {
	if n.raft == nil {
		return false
	}
	f := n.raft.GetConfiguration()
	if err := f.Error(); err != nil {
		return false
	}
	for _, s := range f.Configuration().Servers {
		if string(s.ID) == n.localID {
			return s.Suffrage == hraft.Voter
		}
	}
	return false
}

// Barrier issues a barrier to flush pending log entries.
func (n *Node) Barrier(timeout time.Duration) error {
	if n.raft == nil {
		return raftapi.ErrNotRunning
	}
	f := n.raft.Barrier(timeout)
	return n.translateError(f.Error())
}

// Bootstrap forms a fresh raft cluster from the given NodeIDs as voters.
// Under the mesh transport, the raft ServerAddress equals the NodeID.
// Called by the gossip-driven bootstrap watcher once exactly
// BootstrapExpect raft-eligible peers are stably visible — each peer
// derives the same sorted list and calls Bootstrap with it, so every
// node stamps an identical Configuration at log index 1.
//
// Idempotent: returns nil if raft has already been bootstrapped (e.g. the
// leader's reconciler added us via AddVoter before our watcher fired) or
// if a peer's bootstrap propagated to us first. The "already bootstrapped"
// path is the dominant one for nodes joining an existing cluster.
func (n *Node) Bootstrap(voterIDs []string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.started || n.raft == nil {
		return raftapi.ErrNotRunning
	}
	hasState, err := hraft.HasExistingState(n.logStore, n.stableStore, n.snapStore)
	if err != nil {
		return fmt.Errorf("check existing raft state: %w", err)
	}
	if hasState {
		return nil
	}
	// Under the mesh transport a peer's raft address equals its NodeID, and
	// transport.LocalAddr() already returns ServerAddress(localID); resolve
	// self through it and address every other voter by its NodeID.
	localAddr := n.transport.LocalAddr()
	hsrv := make([]hraft.Server, 0, len(voterIDs))
	for _, id := range voterIDs {
		addr := hraft.ServerAddress(id)
		if id == n.localID {
			addr = localAddr
		}
		hsrv = append(hsrv, hraft.Server{
			Suffrage: hraft.Voter,
			ID:       hraft.ServerID(id),
			Address:  addr,
		})
	}
	f := n.raft.BootstrapCluster(hraft.Configuration{Servers: hsrv})
	if err := f.Error(); err != nil {
		return fmt.Errorf("raft bootstrap: %w", err)
	}
	n.logger.Info("raft cluster bootstrapped",
		zap.String("id", n.localID),
		zap.Int("initial_size", len(hsrv)))
	return nil
}

// AddVoter adds a voting member to the cluster.
func (n *Node) AddVoter(id raftapi.ServerID, addr raftapi.ServerAddress, timeout time.Duration) error {
	if n.raft == nil {
		return raftapi.ErrNotRunning
	}
	f := n.raft.AddVoter(hraft.ServerID(id), hraft.ServerAddress(addr), 0, timeout)
	if err := n.translateError(f.Error()); err != nil {
		return err
	}

	n.sampleVoterLadder()
	return nil
}

// AddNonvoter adds a non-voting (learner) member to the cluster.
// Non-voters receive log replication but do not affect quorum.
func (n *Node) AddNonvoter(id raftapi.ServerID, addr raftapi.ServerAddress, timeout time.Duration) error {
	if n.raft == nil {
		return raftapi.ErrNotRunning
	}
	f := n.raft.AddNonvoter(hraft.ServerID(id), hraft.ServerAddress(addr), 0, timeout)
	if err := n.translateError(f.Error()); err != nil {
		return err
	}

	n.sampleVoterLadder()
	return nil
}

// DemoteVoter demotes an existing voter to a non-voter.
func (n *Node) DemoteVoter(id raftapi.ServerID, timeout time.Duration) error {
	if n.raft == nil {
		return raftapi.ErrNotRunning
	}
	f := n.raft.DemoteVoter(hraft.ServerID(id), 0, timeout)
	if err := n.translateError(f.Error()); err != nil {
		return err
	}

	n.sampleVoterLadder()
	return nil
}

// RemoveServer removes a member from the cluster.
func (n *Node) RemoveServer(id raftapi.ServerID, timeout time.Duration) error {
	if n.raft == nil {
		return raftapi.ErrNotRunning
	}
	f := n.raft.RemoveServer(hraft.ServerID(id), 0, timeout)
	if err := n.translateError(f.Error()); err != nil {
		return err
	}

	n.sampleVoterLadder()
	return nil
}

// LeadershipTransfer transfers leadership to another voter.
// When id is empty, Raft picks a target automatically.
func (n *Node) LeadershipTransfer(id raftapi.ServerID, timeout time.Duration) error {
	if n.raft == nil {
		return raftapi.ErrNotRunning
	}
	var f hraft.Future
	if id == "" {
		f = n.raft.LeadershipTransfer()
	} else {
		// LeadershipTransferToServer requires both ID and address. We don't
		// always know the address from the caller, so look it up in the
		// current configuration.
		cfgFuture := n.raft.GetConfiguration()
		if err := cfgFuture.Error(); err != nil {
			return n.translateError(err)
		}
		var addr hraft.ServerAddress
		for _, s := range cfgFuture.Configuration().Servers {
			if string(s.ID) == id {
				addr = s.Address
				break
			}
		}
		if addr == "" {
			return raftapi.ErrServerNotFound
		}
		f = n.raft.LeadershipTransferToServer(hraft.ServerID(id), addr)
	}
	return n.translateError(awaitFutureWithTimeout(f, timeout))
}

// awaitFutureWithTimeout waits up to timeout for f.Error() to return.
//
// Goroutine lifecycle note: hraft.Future has no cancellation API, so the
// helper goroutine is pinned inside f.Error() until the future resolves.
// The channel is buffered(1), so the goroutine can always send and exit
// once the future does resolve — it is never blocked on the send itself.
// Under a network partition the future stays pending until either the
// partition heals or the Raft instance shuts down (at which point hraft
// closes all pending futures with ErrRaftShutdown).
//
// Cost: one goroutine + one buffered channel per timed-out transfer.
// Transfers are rare cluster-management operations, so this is bounded
// and acceptable.
func awaitFutureWithTimeout(f hraft.Future, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		// done is buffered(1): even if the caller already timed out, this
		// send always succeeds immediately and the goroutine exits.
		done <- f.Error()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return raftapi.ErrTimeout
	}
}

// LogHeadEntry is the cross-pod-comparable view of one raft log entry —
// (index, term) identifies the entry by the Raft log-matching property,
// Hash is sha256(data) so peers can detect divergence without shipping
// payloads. Type lets harnesses skip non-FSM entries (config changes).
type LogHeadEntry struct {
	Type  string `json:"type"`
	Hash  string `json:"hash"`
	Index uint64 `json:"index"`
	Term  uint64 `json:"term"`
}

// LogHead returns the last n committed entries from the local log store,
// newest last. Used by the chaos harness to verify the Raft log-matching
// invariant (if two pods share (index, term) at the same row, every
// preceding row must match too).
//
// The upper bound is the commit index, NOT the log's LastIndex —
// Raft's Log Matching Property only applies to committed entries.
// The uncommitted suffix on a follower may legitimately differ from
// the leader's after a partition heal, until the new leader's
// AppendEntries overwrites it. Including uncommitted entries here
// produces false-positive divergence reports in cross-pod comparison.
//
// Read order matters under chaos: we read commit_index FIRST and use
// it as the upper bound. Because CommitIndex monotonically grows
// within an incarnation, an early read gives us a snapshot that
// survives any concurrent log growth. The alternative (read
// LastIndex first, then commit) is also safe per Raft, but reading
// commit first matches the documented invariant of "return at most
// `commit` entries" without depending on the log store's state.
//
// Returns an empty slice if Raft hasn't started, the log store is
// empty, or nothing is committed yet. Caller bounds n; this method
// does not paginate.
func (n *Node) LogHead(want int) ([]LogHeadEntry, error) {
	if n.raft == nil || n.logStore == nil {
		return nil, raftapi.ErrNotRunning
	}
	if want <= 0 {
		return nil, nil
	}
	commit := n.CommitIndex()
	if commit == 0 {
		return nil, nil
	}
	last, err := n.logStore.LastIndex()
	if err != nil {
		return nil, fmt.Errorf("logstore last index: %w", err)
	}
	if last == 0 {
		return nil, nil
	}
	// Defensive: clamp to the smaller of the two. Even if commit has
	// raced ahead of the log store's view (it shouldn't, but Stats is
	// a separate map read), the resulting bound is still <= some
	// real durable commit point.
	if commit < last {
		last = commit
	}
	first, err := n.logStore.FirstIndex()
	if err != nil {
		return nil, fmt.Errorf("logstore first index: %w", err)
	}
	startWant := uint64(0)
	if uint64(want) >= last {
		startWant = first
	} else {
		startWant = last - uint64(want) + 1
		if startWant < first {
			startWant = first
		}
	}
	out := make([]LogHeadEntry, 0, last-startWant+1)
	for i := startWant; i <= last; i++ {
		var le hraft.Log
		if err := n.logStore.GetLog(i, &le); err != nil {
			// Holes in the log are possible after a snapshot;
			// skip the missing index rather than fail the whole read.
			continue
		}
		sum := sha256.Sum256(le.Data)
		out = append(out, LogHeadEntry{
			Index: le.Index,
			Term:  le.Term,
			Type:  le.Type.String(),
			Hash:  hex.EncodeToString(sum[:8]),
		})
	}
	return out, nil
}

// IsMember reports whether the local node appears in its own committed
// Raft configuration as a voter or non-voter. Reads the local committed
// config only; does not gossip or publish. Returns false when raft is
// not running or the configuration is unreadable.
func (n *Node) IsMember() bool {
	n.mu.Lock()
	r := n.raft
	n.mu.Unlock()
	if r == nil {
		return false
	}
	f := r.GetConfiguration()
	if err := f.Error(); err != nil {
		return false
	}
	for _, s := range f.Configuration().Servers {
		if string(s.ID) == n.localID {
			return true
		}
	}
	return false
}

// Role returns a single-word description of the local node's relationship
// to the Raft cluster, composed from IsLeader plus the local suffrage in
// the committed config: "leader" | "voter" | "standby" | "non-member".
// "standby" denotes a non-voting (learner) member. Pure local read.
func (n *Node) Role() string {
	n.mu.Lock()
	r := n.raft
	n.mu.Unlock()
	if r == nil {
		return "non-member"
	}
	leader := r.State() == hraft.Leader
	f := r.GetConfiguration()
	if err := f.Error(); err != nil {
		if leader {
			return "leader"
		}
		return "non-member"
	}
	var suffrage hraft.ServerSuffrage = -1
	found := false
	for _, s := range f.Configuration().Servers {
		if string(s.ID) == n.localID {
			suffrage = s.Suffrage
			found = true
			break
		}
	}
	if leader {
		return "leader"
	}
	if !found {
		return "non-member"
	}
	switch suffrage {
	case hraft.Voter:
		return "voter"
	case hraft.Nonvoter:
		return "standby"
	default:
		return "non-member"
	}
}

// Stats returns the underlying Raft node's runtime statistics snapshot.
// Returns nil when raft is not running.
func (n *Node) Stats() map[string]string {
	n.mu.Lock()
	r := n.raft
	n.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.Stats()
}

// GetConfiguration returns the current Raft cluster membership.
func (n *Node) GetConfiguration() ([]raftapi.Server, error) {
	if n.raft == nil {
		return nil, raftapi.ErrNotRunning
	}
	f := n.raft.GetConfiguration()
	if err := f.Error(); err != nil {
		return nil, n.translateError(err)
	}
	servers := f.Configuration().Servers
	result := make([]raftapi.Server, len(servers))
	for i, s := range servers {
		result[i] = raftapi.Server{
			ID:      string(s.ID),
			Address: string(s.Address),
			IsVoter: s.Suffrage == hraft.Voter,
		}
	}
	return result, nil
}

// translateError converts hashicorp/raft errors to our API errors.
func (n *Node) translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, hraft.ErrNotLeader) {
		return raftapi.ErrNotLeader
	}
	if errors.Is(err, hraft.ErrLeadershipLost) {
		return raftapi.ErrLeadershipLost
	}
	if errors.Is(err, hraft.ErrRaftShutdown) {
		return raftapi.ErrNotRunning
	}
	return err
}

// Ensure Node implements raft.Service.
var _ raftapi.Service = (*Node)(nil)

// instrumentedTransport wraps an hraft.Transport so AppendEntries calls are
// timed and counted via telemetry. All other methods fall through to the
// embedded transport.
type instrumentedTransport struct {
	hraft.Transport
	tel *telemetry
}

func (it *instrumentedTransport) AppendEntries(id hraft.ServerID, target hraft.ServerAddress,
	args *hraft.AppendEntriesRequest, resp *hraft.AppendEntriesResponse) error {
	// Per-AE span removed: under election storms (term=10000+) this path
	// generated hundreds of spans/sec, swamping the bounded batcher and
	// allocating an attribute slice per call. Coverage is preserved via
	// the counter+histogram below; per-trace sampling at lower-rate
	// upstream paths still yields traces when needed.
	start := time.Now()
	err := it.Transport.AppendEntries(id, target, args, resp)
	it.tel.recordAppendEntries(string(id), err, time.Since(start))
	return err
}

// Close forwards to the underlying transport when it supports closing,
// allowing the wrapper to satisfy hraft.WithClose transparently.
func (it *instrumentedTransport) Close() error {
	if closer, ok := it.Transport.(hraft.WithClose); ok {
		return closer.Close()
	}

	return nil
}

// AppendEntriesPipeline wraps the inner pipeline so streaming AE replications
// also emit raft_append_entries_* metrics. Without this wrap, steady-state
// raft traffic (which uses the pipeline path) is invisible.
func (it *instrumentedTransport) AppendEntriesPipeline(id hraft.ServerID,
	target hraft.ServerAddress) (hraft.AppendPipeline, error) {
	inner, err := it.Transport.AppendEntriesPipeline(id, target)
	if err != nil {
		return nil, err
	}
	return &instrumentedPipeline{AppendPipeline: inner, tel: it.tel, peer: string(id)}, nil
}

// RequestVote wraps RequestVote so leader-election RPCs are visible too.
func (it *instrumentedTransport) RequestVote(id hraft.ServerID, target hraft.ServerAddress,
	args *hraft.RequestVoteRequest, resp *hraft.RequestVoteResponse) error {
	start := time.Now()
	err := it.Transport.RequestVote(id, target, args, resp)
	it.tel.recordRequestVote(string(id), err, time.Since(start))
	return err
}

// RequestPreVote forwards pre-vote RPCs to the inner transport when it
// supports them, so the wrapper does not silently downgrade pre-vote.
//
// Why this is explicit: instrumentedTransport embeds the hraft.Transport
// INTERFACE, whose method set does not include RequestPreVote (the
// hashicorp/raft team factored RequestPreVote into the WithPreVote
// interface "as it wasn't in the original interface specification").
// Without this method, the wrapper does NOT satisfy hraft.WithPreVote
// — even though the concrete inner (*hraft.NetworkTransport) does.
// The peerStateTracker layer above asserts the inner satisfies
// WithPreVote on every pre-vote RPC; without this method that
// assertion fails and every election emits an error, causing an
// election storm.
func (it *instrumentedTransport) RequestPreVote(id hraft.ServerID, target hraft.ServerAddress,
	args *hraft.RequestPreVoteRequest, resp *hraft.RequestPreVoteResponse) error {
	start := time.Now()
	withPV, ok := it.Transport.(hraft.WithPreVote)
	if !ok {
		// The mesh-backed hraft.NetworkTransport implements WithPreVote,
		// so this branch is not reachable in production. Surface as an
		// error rather than panic so a misconfiguration is visible in
		// metrics rather than crashing the pod.
		err := fmt.Errorf("raft-net: inner transport %T does not implement hraft.WithPreVote", it.Transport)
		it.tel.recordRequestPreVote(string(id), err, time.Since(start))
		return err
	}
	err := withPV.RequestPreVote(id, target, args, resp)
	it.tel.recordRequestPreVote(string(id), err, time.Since(start))
	return err
}

// InstallSnapshot wraps the snapshot transport so we record outgoing snapshot
// pushes alongside FSM-side snapshot persistence.
func (it *instrumentedTransport) InstallSnapshot(id hraft.ServerID, target hraft.ServerAddress,
	args *hraft.InstallSnapshotRequest, resp *hraft.InstallSnapshotResponse, data io.Reader) error {
	start := time.Now()
	err := it.Transport.InstallSnapshot(id, target, args, resp, data)
	it.tel.recordInstallSnapshot(string(id), err, time.Since(start))
	return err
}

// instrumentedPipeline counts each pipelined AppendEntries call. Latency is
// recorded when the response future resolves so the histogram reflects the
// actual round-trip, not just the enqueue cost.
type instrumentedPipeline struct {
	hraft.AppendPipeline
	tel  *telemetry
	peer string
}

func (p *instrumentedPipeline) AppendEntries(args *hraft.AppendEntriesRequest,
	resp *hraft.AppendEntriesResponse) (hraft.AppendFuture, error) {
	start := time.Now()
	fut, err := p.AppendPipeline.AppendEntries(args, resp)
	if err != nil {
		p.tel.recordAppendEntries(p.peer, err, time.Since(start))
		return nil, err
	}
	return &instrumentedFuture{AppendFuture: fut, tel: p.tel, peer: p.peer, start: start}, nil
}

// instrumentedFuture observes the response of a pipelined AppendEntries.
type instrumentedFuture struct {
	hraft.AppendFuture
	start time.Time
	tel   *telemetry
	peer  string
}

func (f *instrumentedFuture) Error() error {
	err := f.AppendFuture.Error()
	f.tel.recordAppendEntries(f.peer, err, time.Since(f.start))
	return err
}

// instrumentedFSM wraps a user-supplied hraft.FSM so Snapshot calls emit
// OTel telemetry. Apply/Restore are forwarded unchanged via the embedded
// FSM; only the snapshot path needs metric/span coverage in this task.
type instrumentedFSM struct {
	hraft.FSM
	tel *telemetry
}

func (i *instrumentedFSM) Snapshot() (hraft.FSMSnapshot, error) {
	start := time.Now()
	snap, err := i.FSM.Snapshot()
	if err != nil {
		i.tel.recordSnapshot(err, time.Since(start), 0)
		return nil, err
	}

	return &instrumentedFSMSnapshot{FSMSnapshot: snap, tel: i.tel, start: start}, nil
}

// instrumentedFSMSnapshot wraps the user FSM's snapshot so Persist is
// timed, sized, and traced. Release is delegated unchanged.
type instrumentedFSMSnapshot struct {
	hraft.FSMSnapshot
	tel   *telemetry
	start time.Time
}

func (s *instrumentedFSMSnapshot) Persist(sink hraft.SnapshotSink) error {
	_, span := s.tel.startSpan(context.Background(), "raft.snapshot")
	defer span.End()

	cw := &countingSink{SnapshotSink: sink}
	err := s.FSMSnapshot.Persist(cw)
	span.SetAttributes(attribute.Int64("raft.snapshot.bytes", cw.bytes))
	s.tel.setSpanError(span, err)
	s.tel.recordSnapshot(err, time.Since(s.start), cw.bytes)
	return err
}

// countingSink wraps an hraft.SnapshotSink so we can record the number of
// bytes the FSM wrote during Persist. Forwards Cancel/Close/ID via embedding.
type countingSink struct {
	hraft.SnapshotSink
	bytes int64
}

func (c *countingSink) Write(p []byte) (int, error) {
	n, err := c.SnapshotSink.Write(p)
	c.bytes += int64(n)
	return n, err
}

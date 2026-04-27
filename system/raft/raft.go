// SPDX-License-Identifier: MPL-2.0

// Package raft provides a Raft consensus node integrated with the wippy cluster.
package raft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	hraft "github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	raftapi "github.com/wippyai/runtime/api/raft"
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
	logger      *zap.Logger
	raft        *hraft.Raft
	stopCh      chan struct{}
	tel         *telemetry
	localID     string
	config      raftapi.Config
	actualPort  int
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

// ActualPort returns the port the Raft transport is actually listening on.
// Only valid after Start().
func (n *Node) ActualPort() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.actualPort
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

	// Resolve port.
	port, err := autoDetectPort(n.config)
	if err != nil {
		return nil, fmt.Errorf("raft port detection: %w", err)
	}
	n.actualPort = port
	n.config.BindPort = port

	// Ensure data directory exists.
	if n.config.DataDir != "" {
		if err := os.MkdirAll(n.config.DataDir, 0o755); err != nil {
			return nil, fmt.Errorf("create raft data dir: %w", err)
		}
	}

	// Create stores.
	if n.config.DataDir != "" {
		logPath, stablePath, snapDir := resolveDataDir(n.config.DataDir)

		logStore, err := raftboltdb.NewBoltStore(logPath)
		if err != nil {
			return nil, fmt.Errorf("create raft log store: %w", err)
		}
		n.logStore = logStore

		stableStore, err := raftboltdb.NewBoltStore(stablePath)
		if err != nil {
			return nil, fmt.Errorf("create raft stable store: %w", err)
		}
		n.stableStore = stableStore

		snapStore, err := hraft.NewFileSnapshotStore(snapDir, n.config.SnapshotRetain, os.Stderr)
		if err != nil {
			return nil, fmt.Errorf("create raft snapshot store: %w", err)
		}
		n.snapStore = snapStore
	} else {
		// In-memory stores for ephemeral/test usage.
		n.logStore = hraft.NewInmemStore()
		n.stableStore = hraft.NewInmemStore()
		n.snapStore = hraft.NewInmemSnapshotStore()
	}

	// Create transport.
	bindAddr := resolveTransportAddr(n.config)
	advertiseAddr := resolveAdvertiseAddr(n.config, port)
	transport, err := hraft.NewTCPTransport(bindAddr, advertiseAddr, n.config.MaxPool, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("create raft transport: %w", err)
	}
	n.transport = &instrumentedTransport{Transport: transport, tel: n.tel}

	// Create Raft instance. The FSM is wrapped so Snapshot/Persist calls
	// emit OTel metrics and spans without leaking knowledge of telemetry
	// into FSM implementations.
	rc := toHashicorpConfig(n.localID, n.config)
	wrappedFSM := &instrumentedFSM{FSM: n.fsm, tel: n.tel}
	r, err := hraft.NewRaft(rc, wrappedFSM, n.logStore, n.stableStore, n.snapStore, n.transport)
	if err != nil {
		transport.Close()
		return nil, fmt.Errorf("create raft instance: %w", err)
	}
	n.raft = r

	// Bootstrap if configured and no existing state.
	if n.config.Bootstrap {
		hasState, err := hraft.HasExistingState(n.logStore, n.stableStore, n.snapStore)
		if err != nil {
			return nil, fmt.Errorf("check existing raft state: %w", err)
		}
		if !hasState {
			cfg := hraft.Configuration{
				Servers: []hraft.Server{
					{
						ID:      hraft.ServerID(n.localID),
						Address: n.transport.LocalAddr(),
					},
				},
			}
			f := r.BootstrapCluster(cfg)
			if err := f.Error(); err != nil {
				n.logger.Warn("raft bootstrap failed (may already be bootstrapped)", zap.Error(err))
			} else {
				n.logger.Info("raft cluster bootstrapped", zap.String("id", n.localID))
			}
		}
	}

	n.started = true

	// Start leadership monitor goroutine.
	statusCh := make(chan any, 4)
	go n.monitorLeadership(statusCh)

	n.logger.Info("raft node started",
		zap.String("id", n.localID),
		zap.String("bind", bindAddr),
		zap.Int("port", port),
		zap.Bool("bootstrap", n.config.Bootstrap))

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

	// Close bolt stores if applicable.
	if bs, ok := n.logStore.(*raftboltdb.BoltStore); ok {
		bs.Close()
	}
	if bs, ok := n.stableStore.(*raftboltdb.BoltStore); ok {
		bs.Close()
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

	// Track election timing: set whenever we leave the leader state, used to
	// compute election duration when we (re)enter leader.
	var electionStart time.Time
	wasLeader := false

	// Initial sample so dashboards see state immediately.
	n.sampleStateAndTerm()

	for {
		select {
		case isLeader, ok := <-leaderCh:
			if !ok {
				return
			}
			if isLeader {
				if !wasLeader && !electionStart.IsZero() {
					n.tel.recordElection(time.Since(electionStart))
				}
				n.tel.recordLeaderChange()
				wasLeader = true
				n.logger.Info("this node is now the raft leader", zap.String("id", n.localID))
				n.bus.Send(context.Background(), event.Event{
					System: cluster.System,
					Kind:   cluster.LeaderElected,
					Path:   n.localID,
				})
			} else {
				wasLeader = false
				electionStart = time.Now()
				n.logger.Info("this node lost raft leadership", zap.String("id", n.localID))
				n.bus.Send(context.Background(), event.Event{
					System: cluster.System,
					Kind:   cluster.LeaderLost,
					Path:   n.localID,
				})
			}
			n.sampleStateAndTerm()
		case <-sampleTicker.C:
			n.sampleStateAndTerm()
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

// Barrier issues a barrier to flush pending log entries.
func (n *Node) Barrier(timeout time.Duration) error {
	if n.raft == nil {
		return raftapi.ErrNotRunning
	}
	f := n.raft.Barrier(timeout)
	return n.translateError(f.Error())
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
	// hashicorp/raft's transfer future has no per-call timeout — wrap it.
	done := make(chan error, 1)
	go func() { done <- f.Error() }()
	select {
	case err := <-done:
		return n.translateError(err)
	case <-time.After(timeout):
		return raftapi.ErrTimeout
	}
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
	_, span := it.tel.startSpan(context.Background(), "raft.append_entries",
		trace.WithAttributes(
			attribute.String("peer", string(id)),
			attribute.Int("entries", len(args.Entries)),
		),
	)
	defer span.End()

	start := time.Now()
	err := it.Transport.AppendEntries(id, target, args, resp)
	it.tel.setSpanError(span, err)
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

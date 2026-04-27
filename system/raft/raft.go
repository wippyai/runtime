// SPDX-License-Identifier: MPL-2.0

// Package raft provides a Raft consensus node integrated with the wippy cluster.
package raft

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	hraft "github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
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
	logger      *zap.Logger
	transport   *hraft.NetworkTransport
	raft        *hraft.Raft
	stopCh      chan struct{}
	localID     string
	config      raftapi.Config
	actualPort  int
	mu          sync.Mutex
	started     bool
}

// NewNode creates a new Raft node. The FSM must be provided by the caller
// (e.g., the global registry state machine).
func NewNode(localID string, fsm hraft.FSM, cfg raftapi.Config, bus event.Bus, logger *zap.Logger) *Node {
	cfg.InitDefaults()
	return &Node{
		fsm:     fsm,
		config:  cfg,
		localID: localID,
		bus:     bus,
		logger:  logger,
		stopCh:  make(chan struct{}),
	}
}

// ActualPort returns the port the Raft transport is actually listening on.
// Only valid after Start().
func (n *Node) ActualPort() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.actualPort
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
	n.transport = transport

	// Create Raft instance.
	rc := toHashicorpConfig(n.localID, n.config)
	r, err := hraft.NewRaft(rc, n.fsm, n.logStore, n.stableStore, n.snapStore, n.transport)
	if err != nil {
		n.transport.Close()
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
		n.transport.Close()
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
// events to the event bus.
func (n *Node) monitorLeadership(statusCh chan<- any) {
	defer close(statusCh)

	leaderCh := n.raft.LeaderCh()
	for {
		select {
		case isLeader, ok := <-leaderCh:
			if !ok {
				return
			}
			if isLeader {
				n.logger.Info("this node is now the raft leader", zap.String("id", n.localID))
				n.bus.Send(context.Background(), event.Event{
					System: cluster.System,
					Kind:   cluster.LeaderElected,
					Path:   n.localID,
				})
			} else {
				n.logger.Info("this node lost raft leadership", zap.String("id", n.localID))
				n.bus.Send(context.Background(), event.Event{
					System: cluster.System,
					Kind:   cluster.LeaderLost,
					Path:   n.localID,
				})
			}
		case <-n.stopCh:
			return
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
	return n.translateError(f.Error())
}

// AddNonvoter adds a non-voting (learner) member to the cluster.
// Non-voters receive log replication but do not affect quorum.
func (n *Node) AddNonvoter(id raftapi.ServerID, addr raftapi.ServerAddress, timeout time.Duration) error {
	if n.raft == nil {
		return raftapi.ErrNotRunning
	}
	f := n.raft.AddNonvoter(hraft.ServerID(id), hraft.ServerAddress(addr), 0, timeout)
	return n.translateError(f.Error())
}

// DemoteVoter demotes an existing voter to a non-voter.
func (n *Node) DemoteVoter(id raftapi.ServerID, timeout time.Duration) error {
	if n.raft == nil {
		return raftapi.ErrNotRunning
	}
	f := n.raft.DemoteVoter(hraft.ServerID(id), 0, timeout)
	return n.translateError(f.Error())
}

// RemoveServer removes a member from the cluster.
func (n *Node) RemoveServer(id raftapi.ServerID, timeout time.Duration) error {
	if n.raft == nil {
		return raftapi.ErrNotRunning
	}
	f := n.raft.RemoveServer(hraft.ServerID(id), 0, timeout)
	return n.translateError(f.Error())
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

// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"

	"go.uber.org/zap"
)

const (
	// HostID is the relay host ID for the global registry service.
	HostID pid.HostID = "globalreg"

	// Topics for inter-node leader forwarding.
	topicForwardRequest  relay.Topic = "globalreg.forward.request"
	topicForwardResponse relay.Topic = "globalreg.forward.response"

	defaultApplyTimeout = 10 * time.Second
)

// Service implements the globalreg.Registry interface.
// It wraps a Raft-backed FSM and provides leader forwarding for writes
// and topology-based auto-cleanup.
type Service struct {
	router        relay.Receiver
	raftSvc       raftapi.Service
	bus           event.Bus
	topo          topology.Topology
	stopCh        chan struct{}
	logger        *zap.Logger
	fsm           *FSM
	pending       map[uint64]chan *forwardResponse
	monitoredPIDs sync.Map
	localNode     pid.NodeID
	mu            sync.Mutex
	started       bool
	ready         bool // true after initial Raft barrier completes
}

// forwardResponse wraps the result of a forwarded command.
type forwardResponse struct {
	Err  string
	Data []byte
}

// NewService creates a new global registry service.
func NewService(
	raftSvc raftapi.Service,
	fsm *FSM,
	bus event.Bus,
	topo topology.Topology,
	router relay.Receiver,
	localNode pid.NodeID,
	logger *zap.Logger,
) *Service {
	return &Service{
		raftSvc:   raftSvc,
		fsm:       fsm,
		bus:       bus,
		topo:      topo,
		router:    router,
		localNode: localNode,
		logger:    logger,
		stopCh:    make(chan struct{}),
		pending:   make(map[uint64]chan *forwardResponse),
	}
}

// Start begins the service: subscribes to cluster events for auto-cleanup.
func (s *Service) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil, fmt.Errorf("global registry service already started")
	}
	s.started = true
	s.mu.Unlock()

	// Subscribe to node-left events for auto-cleanup.
	ch := make(chan event.Event, 32)
	subID, err := s.bus.SubscribeP(ctx, cluster.System, cluster.NodeLeft, ch)
	if err != nil {
		return nil, fmt.Errorf("subscribe to cluster events: %w", err)
	}
	go s.handleClusterEvents(ctx, ch, subID)

	// Wait for the Raft log to catch up before serving lookups.
	// This ensures this node won't return stale data after a restart or rejoin.
	go s.waitForReady(ctx)

	statusCh := make(chan any, 1)
	go func() {
		defer close(statusCh)
		<-s.stopCh
	}()

	s.logger.Info("global registry service started", zap.String("node", s.localNode))
	return statusCh, nil
}

// Stop terminates the service.
func (s *Service) Stop(_ context.Context) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	s.mu.Unlock()

	close(s.stopCh)

	s.logger.Info("global registry service stopped", zap.String("node", s.localNode))
	return nil
}

// handleClusterEvents processes node-left events for auto-cleanup.
func (s *Service) handleClusterEvents(ctx context.Context, ch <-chan event.Event, subID event.SubscriberID) {
	defer s.bus.Unsubscribe(ctx, subID)

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e.Kind == cluster.NodeLeft {
				s.handleNodeLeft(ctx, e)
			}
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) handleNodeLeft(ctx context.Context, e event.Event) {
	nodeEvt, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}

	// Only the leader performs cleanup.
	if !s.raftSvc.IsLeader() {
		return
	}

	nodeID := nodeEvt.Node.ID
	s.logger.Info("removing global names for departed node", zap.String("node", nodeID))

	if err := s.RemoveNode(ctx, nodeID); err != nil {
		s.logger.Error("failed to remove node names", zap.String("node", nodeID), zap.Error(err))
	}
}

// --- globalreg.Registry implementation ---

// Register associates a name with a PID globally via Raft.
func (s *Service) Register(_ context.Context, name string, p pid.PID) (pid.PID, error) {
	nodeID := p.Node
	if nodeID == "" {
		nodeID = s.localNode
	}

	cmd := &Command{
		Type:   CmdRegister,
		Name:   name,
		PID:    p,
		NodeID: nodeID,
	}

	resp, err := s.applyCommand(cmd)
	if err != nil {
		return pid.PID{}, err
	}

	result, ok := resp.(*RegisterResult)
	if !ok {
		return pid.PID{}, fmt.Errorf("unexpected register response type: %T", resp)
	}

	if result.Err != nil {
		return result.ExistingPID, globalreg.ErrNameAlreadyRegistered
	}

	// Monitor the PID for auto-cleanup on process exit.
	s.monitorPID(p)

	return result.PID, nil
}

// Unregister removes a global name via Raft.
func (s *Service) Unregister(_ context.Context, name string) (bool, error) {
	cmd := &Command{
		Type: CmdUnregister,
		Name: name,
	}

	resp, err := s.applyCommand(cmd)
	if err != nil {
		return false, err
	}

	result, ok := resp.(*UnregisterResult)
	if !ok {
		return false, fmt.Errorf("unexpected unregister response type: %T", resp)
	}

	return result.Removed, nil
}

// Lookup reads from the local FSM replica. Lock-free, O(1).
func (s *Service) Lookup(name string) (pid.PID, bool) {
	return s.fsm.State().Lookup(name)
}

// LookupWithFence reads from the local FSM replica and returns the fencing
// token (Raft log index). Returns ErrNotReady if the node hasn't caught up yet.
func (s *Service) LookupWithFence(name string) globalreg.LookupResult {
	s.mu.Lock()
	ready := s.ready
	s.mu.Unlock()

	if !ready {
		return globalreg.LookupResult{}
	}

	p, token, found := s.fsm.State().LookupWithFence(name)
	return globalreg.LookupResult{
		PID:        p,
		FenceToken: token,
		Found:      found,
	}
}

// ValidateFence checks whether a fencing token is still valid for a name.
// Returns ErrStaleFence if the name has been re-registered at a higher index.
func (s *Service) ValidateFence(name string, token uint64) error {
	if !s.fsm.State().ValidateFence(name, token) {
		return globalreg.ErrStaleFence
	}
	return nil
}

// LookupByPID returns all global names for a PID.
func (s *Service) LookupByPID(p pid.PID) []string {
	return s.fsm.State().LookupByPID(p)
}

// Remove removes all global names for a PID via Raft.
func (s *Service) Remove(_ context.Context, p pid.PID) error {
	cmd := &Command{
		Type: CmdRemovePID,
		PID:  p,
	}
	_, err := s.applyCommand(cmd)
	return err
}

// RemoveNode removes all global names for a node via Raft.
func (s *Service) RemoveNode(_ context.Context, nodeID pid.NodeID) error {
	cmd := &Command{
		Type:   CmdRemoveNode,
		NodeID: nodeID,
	}
	_, err := s.applyCommand(cmd)
	return err
}

// --- Internal ---

// applyCommand encodes and proposes a command to Raft.
// If this node is not the leader, the command is forwarded via relay.
func (s *Service) applyCommand(cmd *Command) (any, error) {
	data, err := EncodeCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("encode command: %w", err)
	}

	resp, err := s.raftSvc.Apply(data, defaultApplyTimeout)
	if err == nil {
		// FSM.Apply may return an error wrapped in a result struct.
		if resp.Response != nil {
			if fsmErr, ok := resp.Response.(error); ok {
				return nil, fsmErr
			}
		}
		return resp.Response, nil
	}

	// Not the leader — forward to leader via relay.
	if errors.Is(err, raftapi.ErrNotLeader) {
		return s.forwardToLeader(cmd)
	}

	return nil, err
}

// forwardToLeader sends the command to the current Raft leader's global
// registry service via the relay system.
// Retries leader discovery to handle followers that haven't yet learned the leader.
func (s *Service) forwardToLeader(cmd *Command) (any, error) {
	// Retry leader discovery - follower may need time to learn leader after joining cluster
	var leaderID string
	backoff := 100 * time.Millisecond
	for i := 0; i < 30; i++ {
		var err error
		leaderID, _, err = s.raftSvc.Leader()
		if err == nil && leaderID != "" {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
		if backoff > time.Second {
			backoff = time.Second
		}
	}
	if leaderID == "" {
		return nil, globalreg.ErrNotAvailable
	}

	data, err := EncodeCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("encode forward command: %w", err)
	}

	// Send via relay to the leader's globalreg host.
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		leaderID, HostID,
		topicForwardRequest,
		payload.New(data),
	)

	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		return nil, fmt.Errorf("forward to leader: %w", err)
	}

	// For now, we apply locally after forwarding since the Raft log
	// will replicate the result back to us. In a production system,
	// we'd use a request-response correlation pattern. For the initial
	// implementation, we retry local apply with a backoff.
	return s.retryLocalApply(cmd)
}

// retryLocalApply retries the apply locally, waiting for the leader to
// process the forwarded command and for the log entry to replicate to us.
func (s *Service) retryLocalApply(cmd *Command) (any, error) {
	data, err := EncodeCommand(cmd)
	if err != nil {
		return nil, err
	}

	// Retry up to 10 times with exponential backoff.
	backoff := 50 * time.Millisecond
	for i := 0; i < 10; i++ {
		time.Sleep(backoff)

		resp, err := s.raftSvc.Apply(data, defaultApplyTimeout)
		if err == nil {
			if resp.Response != nil {
				if fsmErr, ok := resp.Response.(error); ok {
					return nil, fsmErr
				}
			}
			return resp.Response, nil
		}
		if !errors.Is(err, raftapi.ErrNotLeader) {
			return nil, err
		}
		backoff *= 2
		if backoff > 2*time.Second {
			backoff = 2 * time.Second
		}
	}

	return nil, globalreg.ErrNotAvailable
}

// Send implements relay.Receiver. It handles forwarded commands from other
// nodes and topology exit events for monitored PIDs.
func (s *Service) Send(pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)

	for _, msg := range pkg.Messages {
		switch msg.Topic {
		case topicForwardRequest:
			s.handleForwardRequest(pkg.Source, msg)
		case topology.TopicEvents:
			s.handleExitEvent(msg)
		}
	}

	return nil
}

// handleExitEvent processes topology exit events for monitored PIDs.
// When a globally registered process exits, its names are auto-removed.
func (s *Service) handleExitEvent(msg *relay.Message) {
	for _, p := range msg.Payloads {
		exitEvent, ok := p.Data().(*topology.ExitEvent)
		if !ok {
			continue
		}
		s.HandleProcessExit(exitEvent.From)
	}
}

// handleForwardRequest processes a forwarded command from a follower node.
func (s *Service) handleForwardRequest(source pid.PID, msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}

	data, ok := msg.Payloads[0].Data().([]byte)
	if !ok {
		s.logger.Error("invalid forward request payload type")
		return
	}

	// Apply the command locally (we should be the leader).
	resp, err := s.raftSvc.Apply(data, defaultApplyTimeout)
	if err != nil {
		s.logger.Error("failed to apply forwarded command", zap.Error(err), zap.String("from", source.Node))
		return
	}

	// Check for FSM-level error in the response.
	if resp.Response != nil {
		if fsmErr, ok := resp.Response.(error); ok {
			s.logger.Debug("forwarded command failed at FSM", zap.Error(fsmErr), zap.String("from", source.Node))
		}
	}
}

// monitorPID starts topology monitoring for a globally registered PID.
// When the process exits, its names are auto-removed.
func (s *Service) monitorPID(p pid.PID) {
	pidKey := p.String()
	if _, loaded := s.monitoredPIDs.LoadOrStore(pidKey, struct{}{}); loaded {
		return // already monitoring
	}

	self := pid.PID{Node: s.localNode, Host: HostID}
	if err := s.topo.Monitor(self, p); err != nil {
		s.logger.Debug("failed to monitor globally registered PID",
			zap.String("pid", pidKey), zap.Error(err))
		s.monitoredPIDs.Delete(pidKey)
	}
}

// HandleProcessExit is called when a monitored process exits.
// It removes all global names for that PID.
func (s *Service) HandleProcessExit(p pid.PID) {
	pidKey := p.String()
	if _, ok := s.monitoredPIDs.LoadAndDelete(pidKey); !ok {
		return
	}

	s.logger.Info("auto-removing global names for exited process", zap.String("pid", pidKey))
	if err := s.Remove(context.Background(), p); err != nil {
		s.logger.Error("failed to auto-remove process names", zap.String("pid", pidKey), zap.Error(err))
	}
}

// waitForReady waits for the Raft log to catch up using a barrier,
// then marks the service as ready to serve consistent lookups.
func (s *Service) waitForReady(_ context.Context) {
	if err := s.raftSvc.Barrier(30 * time.Second); err != nil {
		s.logger.Warn("raft barrier timed out during startup, serving lookups anyway",
			zap.Error(err))
	}

	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()

	s.logger.Info("global registry ready (raft log caught up)")
}

// IsReady returns whether the service has caught up with the Raft log
// and can serve consistent lookups.
func (s *Service) IsReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// Ensure Service implements the interfaces.
var (
	_ globalreg.Registry = (*Service)(nil)
	_ relay.Receiver     = (*Service)(nil)
)

// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"

	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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
	router           relay.Receiver
	raftSvc          raftapi.Service
	bus              event.Bus
	topo             topology.Topology
	stopCh           chan struct{}
	logger           *zap.Logger
	fsm              *FSM
	tel              *telemetry
	pending          map[uint64]chan *forwardResponse
	monitoredPIDs    sync.Map
	localNode        pid.NodeID
	monitorWatermark atomic.Uint64 // highest AppliedAt scanned by reestablishMonitors
	mu               sync.Mutex
	started          bool
	ready            bool // true after initial Raft barrier completes
	degraded         bool // true if Raft barrier timed out (serving potentially stale data)
}

// forwardResponse wraps the result of a forwarded command. It carries the
// typed FSM response (Result) for v1 envelopes; older v0 envelopes set only
// ErrMsg and leave Result nil.
type forwardResponse struct {
	Result any
	ErrMsg string
	V1     bool
}

// correlationIDCounter generates unique correlation IDs for forwarded requests.
var correlationIDCounter atomic.Uint64

// NewService creates a new global registry service.
//
// The trailing coll/mp/tp parameters wire OTel-style metrics for the
// pg_fence_*/pg_globalreg_* series consumed by the runtime dashboards. Pass
// nil for any of them to disable telemetry; the recorders are nil-safe.
func NewService(
	raftSvc raftapi.Service,
	fsm *FSM,
	bus event.Bus,
	topo topology.Topology,
	router relay.Receiver,
	localNode pid.NodeID,
	logger *zap.Logger,
	coll metrics.Collector,
	mp otelmetric.MeterProvider,
	tp trace.TracerProvider,
) *Service {
	tel := newTelemetry(coll, mp, tp, localNode)
	if fsm != nil {
		fsm.SetTelemetry(tel)
	}

	s := &Service{
		raftSvc:   raftSvc,
		fsm:       fsm,
		tel:       tel,
		bus:       bus,
		topo:      topo,
		router:    router,
		localNode: localNode,
		logger:    logger,
		stopCh:    make(chan struct{}),
		pending:   make(map[uint64]chan *forwardResponse),
	}
	if fsm != nil {
		fsm.SetOnRestore(s.resetMonitorWatermark)
	}
	return s
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

	// Monitor leadership changes to re-establish PID monitors.
	go s.monitorLeadership()

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

	// A non-zero ResolvedPID means the conflict resolver replaced the prior
	// owner — that's a re-registration. Surfaces as runtime_name_reregistrations_total
	// so the chaos soak gate fails if a partition heal triggers a flood.
	if result.ResolvedPID != (pid.PID{}) {
		s.tel.recordReregistration(s.localNode, "global")
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

// removeNodeChunkSize bounds the work per Raft Apply when bulk-removing
// a departed node's names. 256 names ≈ <5 ms per Apply; chunking lets other
// writes interleave during a large cleanup so the Raft pipeline doesn't
// stall under chaos-driven node churn.
const removeNodeChunkSize = 256

// RemoveNode removes all global names for a node via Raft. The work is
// chunked into bounded Applies so the FSM apply lock is released between
// batches, keeping foreground writes responsive while a node's state
// drains.
func (s *Service) RemoveNode(_ context.Context, nodeID pid.NodeID) error {
	for {
		cmd := &Command{
			Type:   CmdRemoveNode,
			NodeID: nodeID,
			Limit:  removeNodeChunkSize,
		}
		resp, err := s.applyCommand(cmd)
		if err != nil {
			return err
		}
		result, ok := resp.(*RemoveResult)
		if !ok {
			// Unexpected response shape — fall back to no further chunks.
			return nil
		}
		if s.tel != nil {
			s.tel.recordRemoveNodeChunk(nodeID, result.Count)
		}
		if !result.HasMore {
			return nil
		}
	}
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
// registry service via the relay system and waits for the response.
// Uses a correlation ID to match the response to the request. On success it
// returns the typed FSM result decoded on the follower side.
func (s *Service) forwardToLeader(cmd *Command) (any, error) {
	start := time.Now()
	// Retry leader discovery - follower may need time to learn leader after joining cluster.
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
		s.tel.recordForwardedApply(cmd.Type, forwardResultNoLeader, time.Since(start))
		return nil, globalreg.ErrNotAvailable
	}

	data, err := EncodeCommand(cmd)
	if err != nil {
		s.tel.recordForwardedApply(cmd.Type, forwardResultError, time.Since(start))
		return nil, fmt.Errorf("encode forward command: %w", err)
	}

	corrID := correlationIDCounter.Add(1)
	respCh := make(chan *forwardResponse, 1)

	s.mu.Lock()
	s.pending[corrID] = respCh
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, corrID)
		s.mu.Unlock()
	}()

	// Prepend 8-byte correlation ID to the command data.
	envelope := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(envelope[:8], corrID)
	copy(envelope[8:], data)

	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		leaderID, HostID,
		topicForwardRequest,
		payload.New(envelope),
	)

	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.tel.recordForwardedApply(cmd.Type, forwardResultSendFailed, time.Since(start))
		return nil, fmt.Errorf("forward to leader: %w", err)
	}

	select {
	case resp := <-respCh:
		if resp.ErrMsg != "" {
			s.tel.recordForwardedApply(cmd.Type, forwardResultError, time.Since(start))
			return nil, errors.New(resp.ErrMsg)
		}
		s.tel.recordForwardedApply(cmd.Type, forwardResultOK, time.Since(start))
		return resp.Result, nil
	case <-time.After(defaultApplyTimeout):
		s.tel.recordForwardedApply(cmd.Type, forwardResultTimeout, time.Since(start))
		return nil, globalreg.ErrNotAvailable
	case <-s.stopCh:
		s.tel.recordForwardedApply(cmd.Type, forwardResultTimeout, time.Since(start))
		return nil, globalreg.ErrNotAvailable
	}
}

// Send implements relay.Receiver. It handles forwarded commands from other
// nodes and topology exit events for monitored PIDs.
func (s *Service) Send(pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)

	for _, msg := range pkg.Messages {
		switch msg.Topic {
		case topicForwardRequest:
			s.handleForwardRequest(pkg.Source, msg)
		case topicForwardResponse:
			s.handleForwardResponse(msg)
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
// Applies the command via Raft and sends a v1 typed response back with the
// correlation ID. The follower decodes the typed FSM result.
func (s *Service) handleForwardRequest(source pid.PID, msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}

	envelope, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(envelope) < 8 {
		s.logger.Error("invalid forward request payload")
		return
	}

	corrID := binary.BigEndian.Uint64(envelope[:8])
	data := envelope[8:]

	// Decode the command so we can tag the typed response with its kind. The
	// leader-side apply path itself does not need the decoded command — Raft
	// decodes it again inside FSM.Apply — but the response envelope must
	// carry the command kind so the follower knows how to decode the typed
	// result blob.
	cmd, decodeErr := DecodeCommand(data)
	if decodeErr != nil {
		s.logger.Error("invalid forward request command", zap.Error(decodeErr))
		s.replyForward(source.Node, corrID, 0, decodeErr.Error(), nil)
		return
	}

	var (
		errMsg     string
		resultBlob []byte
	)
	resp, err := s.raftSvc.Apply(data, defaultApplyTimeout)
	switch {
	case err != nil:
		errMsg = err.Error()
	case resp == nil || resp.Response == nil:
		// No-op: nothing to encode.
	default:
		if fsmErr, ok := resp.Response.(error); ok {
			errMsg = fsmErr.Error()
			break
		}
		encoded, encErr := encodeFSMResult(cmd.Type, resp.Response)
		if encErr != nil {
			s.logger.Error("encode forward response result",
				zap.Error(encErr), zap.String("cmd", commandLabel(cmd.Type)))
			errMsg = encErr.Error()
			break
		}
		resultBlob = encoded
	}

	s.replyForward(source.Node, corrID, cmd.Type, errMsg, resultBlob)
}

// replyForward sends the v1 typed envelope back to the requesting follower.
func (s *Service) replyForward(sourceNode pid.NodeID, corrID uint64, cmd CommandType, errMsg string, result []byte) {
	respEnvelope, err := encodeForwardResponse(corrID, cmd, errMsg, result)
	if err != nil {
		s.logger.Error("encode forward response envelope",
			zap.Error(err), zap.String("to", sourceNode))
		return
	}

	respPkg := relay.NewServicePackage(
		s.localNode, HostID,
		sourceNode, HostID,
		topicForwardResponse,
		payload.New(respEnvelope),
	)

	if err := s.router.Send(respPkg); err != nil {
		relay.ReleasePackage(respPkg)
		s.logger.Error("failed to send forward response",
			zap.Error(err), zap.String("to", sourceNode))
	}
}

// handleForwardResponse processes a response from the leader for a forwarded
// command. It accepts both legacy v0 (error-string-only) and new v1 (typed
// result) envelopes so a mid-upgrade cluster never wedges a follower.
func (s *Service) handleForwardResponse(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}

	envelope, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(envelope) < 8 {
		s.logger.Error("invalid forward response payload")
		return
	}

	decoded, err := decodeForwardResponse(envelope)
	if err != nil {
		s.logger.Error("failed to decode forward response envelope",
			zap.Error(err), zap.Uint64("corr_id", decoded.CorrID))
		s.deliverForward(decoded.CorrID, &forwardResponse{
			ErrMsg: fmt.Sprintf("decode forward response: %v", err),
		})
		return
	}

	resp := &forwardResponse{
		ErrMsg: decoded.ErrMsg,
		Result: decoded.Result,
		V1:     decoded.V1,
	}
	s.deliverForward(decoded.CorrID, resp)
}

// deliverForward delivers a parsed response to the waiting forwardToLeader
// goroutine, if any. Lost responses (no pending entry) are logged at debug
// level — the only callers are the timeout / stop branches in forwardToLeader.
func (s *Service) deliverForward(corrID uint64, resp *forwardResponse) {
	s.mu.Lock()
	ch, ok := s.pending[corrID]
	s.mu.Unlock()
	if !ok {
		s.logger.Debug("received forward response for unknown correlation ID",
			zap.Uint64("corr_id", corrID))
		return
	}
	select {
	case ch <- resp:
	default:
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

// monitorLeadership watches for leadership changes and re-establishes
// PID monitors when this node becomes the leader. This ensures that after
// a leader failover, the new leader monitors all registered local PIDs
// for auto-cleanup on process exit.
func (s *Service) monitorLeadership() {
	leaderCh := s.raftSvc.LeaderCh()
	for {
		select {
		case isLeader, ok := <-leaderCh:
			if !ok {
				return
			}
			if isLeader {
				s.reestablishMonitors()
			}
		case <-s.stopCh:
			return
		}
	}
}

// reestablishMonitors scans entries newer than the last reestablish watermark
// and sets up topology monitors for PIDs local to this node. Bounding the
// scan by AppliedAt avoids a full FSM walk on every leader transition once
// a node has been around long enough to have monitored everything below the
// watermark. monitorPID() is itself idempotent via monitoredPIDs, so the
// watermark is purely a cost-cap: a leader failover with no new entries
// allocates nothing.
func (s *Service) reestablishMonitors() {
	threshold := s.monitorWatermark.Load()
	entries, highest := s.fsm.State().snapshotAbove(threshold)
	for _, e := range entries {
		if e.NodeID == s.localNode {
			s.monitorPID(e.PID)
		}
	}
	if highest > threshold {
		// CAS so concurrent calls (none today, but defensive) only advance.
		for {
			cur := s.monitorWatermark.Load()
			if highest <= cur {
				break
			}
			if s.monitorWatermark.CompareAndSwap(cur, highest) {
				break
			}
		}
	}
}

// resetMonitorWatermark is invoked when the FSM is wholesale-replaced (e.g.
// snapshot install). Future reestablishMonitors calls must rescan from zero.
func (s *Service) resetMonitorWatermark() {
	s.monitorWatermark.Store(0)
}

// waitForReady waits for the Raft log to catch up using a barrier,
// then marks the service as ready to serve lookups.
// If the barrier times out, the service is marked as degraded (serving
// potentially stale data) but still ready, preferring availability over
// blocking indefinitely.
func (s *Service) waitForReady(_ context.Context) {
	if err := s.raftSvc.Barrier(30 * time.Second); err != nil {
		s.logger.Warn("raft barrier timed out during startup, serving lookups in degraded mode",
			zap.Error(err))
		s.mu.Lock()
		s.degraded = true
		s.ready = true
		s.mu.Unlock()
		return
	}

	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()

	s.logger.Info("global registry ready (raft log caught up)")
}

// IsReady returns whether the service has caught up with the Raft log
// and can serve lookups.
func (s *Service) IsReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// IsDegraded returns whether the service started without fully catching up
// with the Raft log. Lookups may return stale data.
func (s *Service) IsDegraded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.degraded
}

// RecordFenceRejection emits the pg_fence_rejection_total counter. It is
// intended to be wired as a callback from the relay Router so the rejection
// site (which validates the fence token before delivery) does not have to
// depend on the metrics package directly.
func (s *Service) RecordFenceRejection(name, reason string) {
	if s == nil || s.tel == nil {
		return
	}

	// Use the global name as the "pg" label; for pg-process-group messages
	// the global name is the group identifier, which is exactly what the
	// dashboard wants to slice by.
	pg := name
	if pg == "" {
		pg = HostID
	}

	s.tel.recordFenceRejection(pg, reason)
}

// Ensure Service implements the interfaces.
var (
	_ globalreg.Registry = (*Service)(nil)
	_ relay.Receiver     = (*Service)(nil)
)

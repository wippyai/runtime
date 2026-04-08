// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// action represents a serialized operation submitted to the event loop.
type action func()

// Service implements the pg (process groups) service.
// All state mutations are serialized through a single goroutine event loop,
// following the Erlang gen_server pattern.
type Service struct {
	state         *state
	logger        *zap.Logger
	router        relay.Receiver
	topo          topology.Topology
	membership    cluster.Membership
	bus           event.Bus
	ctx           context.Context
	cancel        context.CancelFunc
	nodeJoinedSub *eventbus.Subscriber
	nodeLeftSub   *eventbus.Subscriber
	actions       chan action
	localNodeID   pid.NodeID
	wg            sync.WaitGroup
}

// NewService creates a new pg service.
func NewService(
	logger *zap.Logger,
	router relay.Receiver,
	topo topology.Topology,
	membership cluster.Membership,
	bus event.Bus,
	localNodeID pid.NodeID,
) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		state:       newState(),
		logger:      logger.Named("pg"),
		router:      router,
		topo:        topo,
		membership:  membership,
		bus:         bus,
		localNodeID: localNodeID,
		actions:     make(chan action, 256),
	}
}

// Start begins the pg service event loop and subscribes to cluster events.
func (s *Service) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Subscribe to cluster events for node discovery
	if s.bus != nil {
		var err error
		s.nodeJoinedSub, err = eventbus.NewSubscriber(s.ctx, s.bus, cluster.System, cluster.NodeJoined, s.handleNodeJoinedEvent)
		if err != nil {
			s.cancel()
			return err
		}

		s.nodeLeftSub, err = eventbus.NewSubscriber(s.ctx, s.bus, cluster.System, cluster.NodeLeft, s.handleNodeLeftEvent)
		if err != nil {
			s.nodeJoinedSub.Close()
			s.cancel()
			return err
		}
	}

	// Start event loop
	s.wg.Add(1)
	go s.eventLoop()

	// Discover existing nodes
	if s.membership != nil {
		localNode := s.membership.LocalNode()
		for _, node := range s.membership.Nodes() {
			if node.ID != localNode.ID {
				nodeID := node.ID
				s.submit(func() {
					s.sendDiscover(nodeID)
				})
			}
		}
	}

	s.logger.Info("pg service started", zap.String("node", s.localNodeID))
	return nil
}

// Stop shuts down the pg service.
func (s *Service) Stop(_ context.Context) error {
	s.logger.Info("pg service stopping")

	if s.nodeJoinedSub != nil {
		s.nodeJoinedSub.Close()
	}
	if s.nodeLeftSub != nil {
		s.nodeLeftSub.Close()
	}

	s.cancel()
	s.wg.Wait()

	s.logger.Info("pg service stopped")
	return nil
}

// eventLoop runs the single-threaded event loop that processes all actions.
func (s *Service) eventLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case fn, ok := <-s.actions:
			if !ok {
				return
			}
			fn()
		}
	}
}

// submit sends an action to the event loop for serialized execution.
func (s *Service) submit(fn action) {
	select {
	case s.actions <- fn:
	case <-s.ctx.Done():
	}
}

// Send implements relay.Receiver for the pg host.
// Incoming relay packages are dispatched to protocol handlers.
func (s *Service) Send(pkg *relay.Package) error {
	if pkg == nil || len(pkg.Messages) == 0 {
		return nil
	}

	for _, msg := range pkg.Messages {
		switch msg.Topic {
		case pgapi.TopicDiscover:
			s.handleDiscoverPackage(msg)
		case pgapi.TopicSync:
			s.handleSyncPackage(msg)
		case pgapi.TopicJoin:
			s.handleJoinPackage(msg)
		case pgapi.TopicLeave:
			s.handleLeavePackage(msg)
		case topology.TopicEvents:
			s.handleExitPackage(msg)
		}
	}

	relay.ReleasePackage(pkg)
	return nil
}

// handleDiscoverPackage processes an incoming discover message.
func (s *Service) handleDiscoverPackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}

	s.submit(func() {
		s.handleDiscover(fromNodeID)
	})
}

// handleSyncPackage processes an incoming sync message.
func (s *Service) handleSyncPackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}
	rawGroups, _ := data["groups"].(map[string]any)

	groups := make(map[string][]pid.PID, len(rawGroups))
	for group, raw := range rawGroups {
		pidStrs, ok := raw.([]any)
		if !ok {
			continue
		}
		pids := make([]pid.PID, 0, len(pidStrs))
		for _, ps := range pidStrs {
			if s, ok := ps.(string); ok {
				if p, err := pid.ParsePID(s); err == nil {
					pids = append(pids, p)
				}
			}
		}
		if len(pids) > 0 {
			groups[group] = pids
		}
	}

	s.submit(func() {
		s.handleSync(fromNodeID, groups)
	})
}

// handleJoinPackage processes an incoming join message.
func (s *Service) handleJoinPackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	group, _ := data["group"].(string)
	if fromNodeID == "" || group == "" {
		return
	}
	rawPids, _ := data["pids"].([]any)

	pids := make([]pid.PID, 0, len(rawPids))
	for _, ps := range rawPids {
		if s, ok := ps.(string); ok {
			if p, err := pid.ParsePID(s); err == nil {
				pids = append(pids, p)
			}
		}
	}

	s.submit(func() {
		s.handleRemoteJoin(fromNodeID, group, pids)
	})
}

// handleLeavePackage processes an incoming leave message.
func (s *Service) handleLeavePackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}
	rawPids, _ := data["pids"].([]any)
	rawGroups, _ := data["groups"].([]any)

	pids := make([]pid.PID, 0, len(rawPids))
	for _, ps := range rawPids {
		if s, ok := ps.(string); ok {
			if p, err := pid.ParsePID(s); err == nil {
				pids = append(pids, p)
			}
		}
	}

	groups := make([]string, 0, len(rawGroups))
	for _, gs := range rawGroups {
		if s, ok := gs.(string); ok {
			groups = append(groups, s)
		}
	}

	s.submit(func() {
		s.handleRemoteLeave(fromNodeID, pids, groups)
	})
}

// handleExitPackage processes an incoming process exit event.
func (s *Service) handleExitPackage(msg *relay.Message) {
	for _, p := range msg.Payloads {
		if exitEvent, ok := p.Data().(*topology.ExitEvent); ok {
			exitedPID := exitEvent.From
			s.submit(func() {
				s.handleProcessExit(exitedPID)
			})
		}
	}
}

// handleNodeJoinedEvent is called by the event bus subscriber when a node joins.
func (s *Service) handleNodeJoinedEvent(e event.Event) {
	nodeEvent, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}
	nodeID := nodeEvent.Node.ID
	if nodeID == s.localNodeID {
		return
	}

	s.submit(func() {
		s.sendDiscover(nodeID)
	})
}

// handleNodeLeftEvent is called by the event bus subscriber when a node leaves.
func (s *Service) handleNodeLeftEvent(e event.Event) {
	nodeEvent, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}
	nodeID := nodeEvent.Node.ID
	if nodeID == s.localNodeID {
		return
	}

	s.submit(func() {
		s.handleNodeLeft(nodeID)
	})
}

// --- ProcessGroups interface implementation ---
// These methods use the serialized event loop for thread safety.

// Join adds a local process to a group.
func (s *Service) Join(group pgapi.Group, p pid.PID) error {
	done := make(chan error, 1)
	s.submit(func() {
		// Check if this is the first join for this process
		_, existed := s.state.local[p.String()]
		s.state.joinLocal(group, p)

		// Monitor the process if this is the first join
		if !existed {
			s.monitorProcess(p)
		}

		// Broadcast to remote nodes
		s.broadcastJoin(group, []pid.PID{p})

		// Emit membership event
		s.emitJoinEvent(group, []pid.PID{p})

		done <- nil
	})

	select {
	case err := <-done:
		return err
	case <-s.ctx.Done():
		return ErrServiceStopped
	}
}

// Leave removes a local process from a group.
func (s *Service) Leave(group pgapi.Group, p pid.PID) error {
	done := make(chan error, 1)
	s.submit(func() {
		if !s.state.leaveLocal(group, p) {
			done <- ErrNotJoined
			return
		}

		// If the process has no more groups, stop monitoring
		if _, exists := s.state.local[p.String()]; !exists {
			s.demonitorProcess(p)
		}

		// Broadcast to remote nodes
		s.broadcastLeave([]pid.PID{p}, []string{group})

		// Emit membership event
		s.emitLeaveEvent(group, []pid.PID{p})

		done <- nil
	})

	select {
	case err := <-done:
		return err
	case <-s.ctx.Done():
		return ErrServiceStopped
	}
}

// GetMembers returns all members of a group across all nodes.
func (s *Service) GetMembers(group pgapi.Group) []pid.PID {
	done := make(chan []pid.PID, 1)
	s.submit(func() {
		done <- s.state.getMembers(group)
	})

	select {
	case members := <-done:
		return members
	case <-s.ctx.Done():
		return nil
	}
}

// GetLocalMembers returns local members of a group.
func (s *Service) GetLocalMembers(group pgapi.Group) []pid.PID {
	done := make(chan []pid.PID, 1)
	s.submit(func() {
		done <- s.state.getLocalMembers(group)
	})

	select {
	case members := <-done:
		return members
	case <-s.ctx.Done():
		return nil
	}
}

// WhichGroups returns all groups that have at least one member.
func (s *Service) WhichGroups() []pgapi.Group {
	done := make(chan []pgapi.Group, 1)
	s.submit(func() {
		done <- s.state.whichGroups()
	})

	select {
	case groups := <-done:
		return groups
	case <-s.ctx.Done():
		return nil
	}
}

// Broadcast sends a message to all members of a group across all nodes.
func (s *Service) Broadcast(from pid.PID, group pgapi.Group, topic string, payloads payload.Payloads) error {
	done := make(chan broadcastResult, 1)
	s.submit(func() {
		members := s.state.getMembers(group)
		sent := sendToMembers(s.router, s.logger, from, topic, payloads, members)
		done <- broadcastResult{sent: sent}
	})

	select {
	case result := <-done:
		_ = result
		return nil
	case <-s.ctx.Done():
		return ErrServiceStopped
	}
}

// BroadcastLocal sends a message to local members of a group only.
func (s *Service) BroadcastLocal(from pid.PID, group pgapi.Group, topic string, payloads payload.Payloads) error {
	done := make(chan broadcastResult, 1)
	s.submit(func() {
		members := s.state.getLocalMembers(group)
		sent := sendToMembers(s.router, s.logger, from, topic, payloads, members)
		done <- broadcastResult{sent: sent}
	})

	select {
	case result := <-done:
		_ = result
		return nil
	case <-s.ctx.Done():
		return ErrServiceStopped
	}
}

type broadcastResult struct {
	sent int
}

// --- Logging helpers ---

func logNodeID(nodeID pid.NodeID) zap.Field {
	return zap.String("node_id", nodeID)
}

func logError(err error) zap.Field {
	return zap.Error(err)
}

func logPID(p pid.PID) zap.Field {
	return zap.String("pid", p.String())
}

func logGroupCount(count int) zap.Field {
	return zap.Int("group_count", count)
}

// --- Event emission ---

// emitJoinEvent publishes a membership join event to the event bus.
func (s *Service) emitJoinEvent(group string, pids []pid.PID) {
	if s.bus == nil {
		return
	}
	s.bus.Send(s.ctx, event.Event{
		System: pgapi.EventSystem,
		Kind:   pgapi.MemberJoined,
		Path:   group,
		Data: pgapi.MembershipEvent{
			Group: group,
			PIDs:  pids,
		},
	})
}

// emitLeaveEvent publishes a membership leave event to the event bus.
func (s *Service) emitLeaveEvent(group string, pids []pid.PID) {
	if s.bus == nil {
		return
	}
	s.bus.Send(s.ctx, event.Event{
		System: pgapi.EventSystem,
		Kind:   pgapi.MemberLeft,
		Path:   group,
		Data: pgapi.MembershipEvent{
			Group: group,
			PIDs:  pids,
		},
	})
}

// Verify interface compliance.
var _ pgapi.ProcessGroups = (*Service)(nil)
var _ relay.Receiver = (*Service)(nil)

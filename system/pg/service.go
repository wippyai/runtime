// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"sync"
	"sync/atomic"

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

// monitorEntry tracks a single group monitor subscription.
type monitorEntry struct {
	pid   pid.PID
	topic string
	id    uint64 // unique ID for unsubscribe
}

// action represents a serialized operation submitted to the event loop.
type action func()

// Service implements the pg (process groups) service.
// All state mutations are serialized through a single goroutine event loop,
// following the Erlang gen_server pattern.
type Service struct {
	router        relay.Receiver
	topo          topology.Topology
	membership    cluster.Membership
	bus           event.Bus
	ctx           context.Context
	state         *state
	logger        *zap.Logger
	cancel        context.CancelFunc
	nodeJoinedSub *eventbus.Subscriber
	nodeLeftSub   *eventbus.Subscriber
	actions       chan action
	monitors      map[string][]*monitorEntry // group -> monitor subscriptions
	snap          atomic.Pointer[stateSnapshot]
	localNodeID   pid.NodeID
	monitorIDSeq  uint64 // monotonic ID for monitors
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
		monitors:    make(map[string][]*monitorEntry),
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

	// Publish initial empty snapshot so lock-free readers see valid data
	s.snap.Store(s.state.buildSnapshot())

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

	// Clear snapshot so lock-free readers see nil after stop
	s.snap.Store(nil)

	s.logger.Info("pg service stopped")
	return nil
}

// eventLoop runs the single-threaded event loop that processes all actions.
// After each action, it publishes an immutable snapshot for lock-free reads.
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

// publishSnapshot rebuilds and atomically stores the state snapshot.
// Must be called inside the event loop (inside an action closure) after
// any state mutation, BEFORE signaling the caller's done channel, so that
// lock-free readers see the updated state immediately.
func (s *Service) publishSnapshot() {
	s.snap.Store(s.state.buildSnapshot())
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
		s.publishSnapshot()
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
		s.publishSnapshot()
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
		s.publishSnapshot()
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
		s.publishSnapshot()
	})
}

// handleExitPackage processes an incoming process exit event.
func (s *Service) handleExitPackage(msg *relay.Message) {
	for _, p := range msg.Payloads {
		if exitEvent, ok := p.Data().(*topology.ExitEvent); ok {
			exitedPID := exitEvent.From
			s.submit(func() {
				s.handleProcessExit(exitedPID)
				s.publishSnapshot()
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
		s.publishSnapshot()
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

		s.publishSnapshot()
		done <- nil
	})

	select {
	case err := <-done:
		return err
	case <-s.ctx.Done():
		return ErrServiceStopped
	}
}

// JoinGroups adds a local process to multiple groups atomically.
func (s *Service) JoinGroups(groups []pgapi.Group, p pid.PID) error {
	done := make(chan error, 1)
	s.submit(func() {
		_, existed := s.state.local[p.String()]

		for _, group := range groups {
			s.state.joinLocal(group, p)
			s.broadcastJoin(group, []pid.PID{p})
			s.emitJoinEvent(group, []pid.PID{p})
		}

		if !existed {
			s.monitorProcess(p)
		}

		s.publishSnapshot()
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

		s.publishSnapshot()
		done <- nil
	})

	select {
	case err := <-done:
		return err
	case <-s.ctx.Done():
		return ErrServiceStopped
	}
}

// LeaveGroups removes a local process from multiple groups.
// Following Erlang PG semantics: leaves all groups where the process is a member,
// skips groups where it isn't, and returns ErrNotJoined only if the process
// was not a member of ANY of the specified groups.
func (s *Service) LeaveGroups(groups []pgapi.Group, p pid.PID) error {
	done := make(chan error, 1)
	s.submit(func() {
		anyLeft := false
		for _, group := range groups {
			if s.state.leaveLocal(group, p) {
				anyLeft = true
				s.broadcastLeave([]pid.PID{p}, []string{group})
				s.emitLeaveEvent(group, []pid.PID{p})
			}
		}

		if !anyLeft {
			done <- ErrNotJoined
			return
		}

		// If the process has no more groups, stop monitoring
		if _, exists := s.state.local[p.String()]; !exists {
			s.demonitorProcess(p)
		}

		s.publishSnapshot()
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
// Uses the RCU snapshot for lock-free reads.
func (s *Service) GetMembers(group pgapi.Group) []pid.PID {
	snap := s.snap.Load()
	if snap == nil {
		return nil
	}
	gs, ok := snap.groups[group]
	if !ok {
		return nil
	}
	return copyPIDs(gs.all)
}

// GetLocalMembers returns local members of a group.
// Uses the RCU snapshot for lock-free reads.
func (s *Service) GetLocalMembers(group pgapi.Group) []pid.PID {
	snap := s.snap.Load()
	if snap == nil {
		return nil
	}
	gs, ok := snap.groups[group]
	if !ok {
		return nil
	}
	return copyPIDs(gs.local)
}

// WhichGroups returns all groups that have at least one member.
// Uses the RCU snapshot for lock-free reads.
func (s *Service) WhichGroups() []pgapi.Group {
	snap := s.snap.Load()
	if snap == nil {
		return nil
	}
	groups := make([]pgapi.Group, 0, len(snap.groups))
	for g := range snap.groups {
		groups = append(groups, g)
	}
	return groups
}

// WhichLocalGroups returns groups that have at least one local member.
// Uses the RCU snapshot for lock-free reads.
func (s *Service) WhichLocalGroups() []pgapi.Group {
	snap := s.snap.Load()
	if snap == nil {
		return nil
	}
	groups := make([]pgapi.Group, 0, len(snap.groups))
	for g, gs := range snap.groups {
		if len(gs.local) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// monitorResult holds the result of a Monitor operation.
type monitorResult struct {
	unsubscribe func()
	members     []pid.PID
}

// Monitor atomically subscribes to a group's membership events and returns
// the current members. Because both operations happen inside a single event
// loop action, no join/leave can interleave between the subscription setup
// and the membership snapshot.
func (s *Service) Monitor(group string, p pid.PID, topic string) monitorResult {
	done := make(chan monitorResult, 1)
	s.submit(func() {
		// Assign unique ID
		s.monitorIDSeq++
		id := s.monitorIDSeq

		entry := &monitorEntry{
			pid:   p,
			topic: topic,
			id:    id,
		}

		s.monitors[group] = append(s.monitors[group], entry)

		// Snapshot current members (while still in event loop — atomic)
		members := s.state.getMembers(group)

		// Build unsubscribe closure
		unsubscribe := func() {
			s.submit(func() {
				s.removeMonitor(group, id)
			})
		}

		done <- monitorResult{members: members, unsubscribe: unsubscribe}
	})

	select {
	case result := <-done:
		return result
	case <-s.ctx.Done():
		return monitorResult{}
	}
}

// removeMonitor removes a monitor entry by ID. Must be called from event loop.
func (s *Service) removeMonitor(group string, id uint64) {
	entries := s.monitors[group]
	for i, e := range entries {
		if e.id == id {
			s.monitors[group] = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	if len(s.monitors[group]) == 0 {
		delete(s.monitors, group)
	}
}

// eventsResult holds the result of an Events operation.
type eventsResult struct {
	groups      map[string][]pid.PID
	unsubscribe func()
}

// Events atomically subscribes to all group membership events and returns
// a snapshot of all current groups and their members. Uses the wildcard
// monitor key ("") to receive events for all groups.
func (s *Service) Events(p pid.PID, topic string) eventsResult {
	done := make(chan eventsResult, 1)
	s.submit(func() {
		s.monitorIDSeq++
		id := s.monitorIDSeq

		entry := &monitorEntry{
			pid:   p,
			topic: topic,
			id:    id,
		}

		// Wildcard key "" matches all groups
		s.monitors[""] = append(s.monitors[""], entry)

		// Snapshot all current groups
		groups := s.state.allGroupMembers()

		unsubscribe := func() {
			s.submit(func() {
				s.removeMonitor("", id)
			})
		}

		done <- eventsResult{groups: groups, unsubscribe: unsubscribe}
	})

	select {
	case result := <-done:
		return result
	case <-s.ctx.Done():
		return eventsResult{}
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

// emitJoinEvent publishes a membership join event to the event bus and delivers to monitors.
func (s *Service) emitJoinEvent(group string, pids []pid.PID) {
	if s.bus != nil {
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

	// Deliver to group monitors
	s.deliverMonitorEvent(group, pgapi.MemberJoined, pids)
}

// emitLeaveEvent publishes a membership leave event to the event bus and delivers to monitors.
func (s *Service) emitLeaveEvent(group string, pids []pid.PID) {
	if s.bus != nil {
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

	// Deliver to group monitors
	s.deliverMonitorEvent(group, pgapi.MemberLeft, pids)
}

// deliverMonitorEvent sends a membership event to all monitors of a specific group
// and to all wildcard monitors. Must be called from the event loop.
func (s *Service) deliverMonitorEvent(group string, kind string, pids []pid.PID) {
	groupEntries := s.monitors[group]
	wildcardEntries := s.monitors[""]
	if len(groupEntries) == 0 && len(wildcardEntries) == 0 {
		return
	}

	// Build event payload matching the eventbus format
	data := map[string]any{
		"system": pgapi.EventSystem,
		"kind":   kind,
		"path":   group,
		"data": pgapi.MembershipEvent{
			Group: group,
			PIDs:  pids,
		},
	}

	deliver := func(entries []*monitorEntry) {
		for _, entry := range entries {
			pkg := relay.NewPackage(pid.PID{}, entry.pid, entry.topic, payload.New(data))
			if err := s.router.Send(pkg); err != nil {
				s.logger.Debug("failed to deliver monitor event",
					zap.String("group", group),
					logPID(entry.pid),
					logError(err),
				)
			}
		}
	}

	deliver(groupEntries)
	deliver(wildcardEntries)
}

// Verify interface compliance.
var _ pgapi.ProcessGroups = (*Service)(nil)
var _ relay.Receiver = (*Service)(nil)

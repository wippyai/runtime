// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/supervisor"
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
//
// Each Service instance represents an independent PG scope (like Erlang's
// pg:start_link(ScopeName)). Scopes have separate state, event loops,
// and cluster mesh — providing complete isolation.
type Service struct {
	router             relay.Receiver
	topo               topology.Topology
	membership         cluster.Membership
	bus                event.Bus
	ctx                context.Context
	snap               atomic.Pointer[stateSnapshot]
	cbManager          *circuitBreakerManager
	cancel             context.CancelFunc
	nodeJoinedSub      *eventbus.Subscriber
	nodeLeftSub        *eventbus.Subscriber
	actions            chan action
	monitors           map[string][]*monitorEntry
	state              *state
	retryQueue         *retryQueue
	logger             *zap.Logger
	localNodeID        pid.NodeID
	hostID             pid.HostID
	wg                 sync.WaitGroup
	monitorIDSeq       uint64
	maxGroups          int
	maxMembersPerGroup int
	actionQueueMaxSize int
	queueWarnThreshold int
	protocolTimeout    time.Duration
	broadcastTimeout   time.Duration
	maxRetries         int
}

// NewService creates a new pg service for the given scope.
//
// The hostID identifies this scope in the relay system and must be unique
// per node. It is derived from the registry entry ID by the Manager.
// The config controls operational parameters like action queue sizing.
func NewService(
	logger *zap.Logger,
	hostID pid.HostID,
	config *pgapi.Config,
	router relay.Receiver,
	topo topology.Topology,
	membership cluster.Membership,
	bus event.Bus,
	localNodeID pid.NodeID,
) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	if config == nil {
		config = &pgapi.Config{}
		config.InitDefaults()
	}

	svc := &Service{
		state:              newState(),
		logger:             logger.Named("pg").Named(hostID),
		hostID:             hostID,
		router:             router,
		topo:               topo,
		membership:         membership,
		bus:                bus,
		localNodeID:        localNodeID,
		actions:            make(chan action, config.ActionQueueSize),
		monitors:           make(map[string][]*monitorEntry),
		maxGroups:          config.MaxGroups,
		maxMembersPerGroup: config.MaxMembersPerGroup,
		actionQueueMaxSize: config.ActionQueueMaxSize,
		queueWarnThreshold: int(float64(config.ActionQueueMaxSize) * 0.75),
		protocolTimeout:    config.ProtocolTimeout,
		broadcastTimeout:   config.BroadcastTimeout,
		maxRetries:         config.MaxRetries,
	}

	// Initialize circuit breaker manager
	svc.cbManager = newCircuitBreakerManager(
		config.CircuitBreakerFailures,
		config.CircuitBreakerResetTime,
		logger,
	)

	// Initialize retry queue
	svc.retryQueue = newRetryQueue(
		svc,
		config.MaxRetries,
		config.RetryBaseDelay,
		config.RetryMaxDelay,
		logger,
	)

	return svc
}

// HostID returns the relay host ID for this PG scope.
func (s *Service) HostID() pid.HostID {
	return s.hostID
}

// Start begins the pg service event loop and subscribes to cluster events.
func (s *Service) Start(ctx context.Context) (<-chan any, error) {
	s.ctx, s.cancel = context.WithCancel(ctx)

	statusChan := make(chan any, 1)

	// Subscribe to cluster events for node discovery
	if s.bus != nil {
		var err error
		s.nodeJoinedSub, err = eventbus.NewSubscriber(s.ctx, s.bus, cluster.System, cluster.NodeJoined, s.handleNodeJoinedEvent)
		if err != nil {
			s.cancel()
			return nil, err
		}

		s.nodeLeftSub, err = eventbus.NewSubscriber(s.ctx, s.bus, cluster.System, cluster.NodeLeft, s.handleNodeLeftEvent)
		if err != nil {
			s.nodeJoinedSub.Close()
			s.cancel()
			return nil, err
		}
	}

	// Publish initial empty snapshot so lock-free readers see valid data
	s.snap.Store(s.state.buildSnapshot())

	// Start event loop
	s.wg.Add(1)
	go s.eventLoop()

	// Start retry queue
	s.retryQueue.Start(s.ctx)

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

	s.logger.Info("pg service started",
		zap.String("node", s.localNodeID),
		zap.Int("queue_size", cap(s.actions)),
		zap.Int("queue_max", s.actionQueueMaxSize),
	)

	select {
	case statusChan <- "pg service started":
	default:
	}

	return statusChan, nil
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

	// Stop retry queue
	if s.retryQueue != nil {
		s.retryQueue.Stop()
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
// Returns true if submitted, false if queue is full (for non-blocking callers).
func (s *Service) submit(fn action) bool {
	// Check queue capacity before attempting to send
	if len(s.actions) >= s.actionQueueMaxSize {
		s.logger.Warn("action queue full, rejecting operation",
			zap.Int("queue_len", len(s.actions)),
			zap.Int("queue_max", s.actionQueueMaxSize),
		)
		return false
	}

	// Warn if queue is approaching capacity
	if len(s.actions) >= s.queueWarnThreshold {
		s.logger.Warn("action queue approaching capacity",
			zap.Int("queue_len", len(s.actions)),
			zap.Int("queue_max", s.actionQueueMaxSize),
			zap.Int("threshold", s.queueWarnThreshold),
		)
	}

	select {
	case s.actions <- fn:
		return true
	case <-s.ctx.Done():
		return false
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
		// Enforce MaxGroups: if the group doesn't exist yet, check limit
		if s.maxGroups > 0 {
			if _, exists := s.state.groups[group]; !exists {
				if len(s.state.groups) >= s.maxGroups {
					done <- pgapi.ErrMaxGroupsReached
					return
				}
			}
		}

		// Enforce MaxMembersPerGroup
		if s.maxMembersPerGroup > 0 {
			if gs, exists := s.state.groups[group]; exists {
				if len(gs.all) >= s.maxMembersPerGroup {
					done <- pgapi.ErrMaxMembersReached
					return
				}
			}
		}

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
		// Pre-check all limits before mutating state (atomic: all-or-nothing)
		if s.maxGroups > 0 || s.maxMembersPerGroup > 0 {
			newGroups := make(map[pgapi.Group]struct{}, len(groups))
			projectedMembers := make(map[pgapi.Group]int, len(groups))
			for _, group := range groups {
				if _, tracked := projectedMembers[group]; !tracked {
					projectedMembers[group] = 0
					if gs, exists := s.state.groups[group]; exists {
						projectedMembers[group] = len(gs.all)
					}
				}

				if s.maxGroups > 0 {
					if _, exists := s.state.groups[group]; !exists {
						newGroups[group] = struct{}{}
					}
				}
				if s.maxMembersPerGroup > 0 {
					projectedMembers[group]++
					if projectedMembers[group] > s.maxMembersPerGroup {
						done <- pgapi.ErrMaxMembersReached
						return
					}
				}
			}
			if s.maxGroups > 0 && len(s.state.groups)+len(newGroups) > s.maxGroups {
				done <- pgapi.ErrMaxGroupsReached
				return
			}
		}

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

// Monitor atomically subscribes to a group's membership events and returns
// the current members. Because both operations happen inside a single event
// loop action, no join/leave can interleave between the subscription setup
// and the membership snapshot.
func (s *Service) Monitor(group string, p pid.PID, topic string) pgapi.MonitorResult {
	done := make(chan pgapi.MonitorResult, 1)
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

		// Monitor the subscriber process so we can clean up if it dies.
		// monitorProcess is idempotent (ignores ErrAlreadyMonitoring).
		s.monitorProcess(p)

		// Snapshot current members (while still in event loop — atomic)
		members := s.state.getMembers(group)

		// Build unsubscribe closure — synchronous: blocks until the event
		// loop processes the removal, so no more events can be emitted after
		// unsubscribe returns (matches Erlang pg:demonitor/2 semantics).
		unsubscribe := func() {
			unsub := make(chan struct{}, 1)
			s.submit(func() {
				s.removeMonitor(group, id)
				// If the process has no more group memberships and no more
				// monitor subscriptions, stop monitoring it.
				if !s.hasMonitorSubscriptions(p) && !s.hasGroupMemberships(p) {
					s.demonitorProcess(p)
				}
				unsub <- struct{}{}
			})
			select {
			case <-unsub:
			case <-s.ctx.Done():
			}
		}

		done <- pgapi.MonitorResult{Members: members, Unsubscribe: unsubscribe}
	})

	select {
	case result := <-done:
		return result
	case <-s.ctx.Done():
		return pgapi.MonitorResult{}
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

// removeMonitorsByPID removes all monitor subscriptions owned by the given PID.
// This mirrors Erlang PG's automatic cleanup when a monitoring process dies.
// Must be called from the event loop.
func (s *Service) removeMonitorsByPID(p pid.PID) {
	key := p.String()
	for group, entries := range s.monitors {
		var remaining []*monitorEntry
		for _, e := range entries {
			if e.pid.String() != key {
				remaining = append(remaining, e)
			}
		}
		if len(remaining) == 0 {
			delete(s.monitors, group)
		} else if len(remaining) != len(entries) {
			s.monitors[group] = remaining
		}
	}
}

// hasMonitorSubscriptions returns true if the given PID has any active
// monitor subscriptions (group-specific or wildcard). Must be called from
// the event loop.
func (s *Service) hasMonitorSubscriptions(p pid.PID) bool {
	key := p.String()
	for _, entries := range s.monitors {
		for _, e := range entries {
			if e.pid.String() == key {
				return true
			}
		}
	}
	return false
}

// hasGroupMemberships returns true if the given PID is a member of any
// local group. Must be called from the event loop.
func (s *Service) hasGroupMemberships(p pid.PID) bool {
	_, exists := s.state.local[p.String()]
	return exists
}

// Events atomically subscribes to all group membership events and returns
// a snapshot of all current groups and their members. Uses the wildcard
// monitor key ("") to receive events for all groups.
func (s *Service) Events(p pid.PID, topic string) pgapi.EventsResult {
	done := make(chan pgapi.EventsResult, 1)
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

		// Monitor the subscriber process so we can clean up if it dies.
		s.monitorProcess(p)

		// Snapshot all current groups
		groups := s.state.allGroupMembers()

		unsubscribe := func() {
			unsub := make(chan struct{}, 1)
			s.submit(func() {
				s.removeMonitor("", id)
				if !s.hasMonitorSubscriptions(p) && !s.hasGroupMemberships(p) {
					s.demonitorProcess(p)
				}
				unsub <- struct{}{}
			})
			select {
			case <-unsub:
			case <-s.ctx.Done():
			}
		}

		done <- pgapi.EventsResult{Groups: groups, Unsubscribe: unsubscribe}
	})

	select {
	case result := <-done:
		return result
	case <-s.ctx.Done():
		return pgapi.EventsResult{}
	}
}

// Broadcast sends a message to all members of a group across all nodes.
func (s *Service) Broadcast(from pid.PID, group pgapi.Group, topic string, payloads payload.Payloads) (int, error) {
	// Snapshot members inside the event loop for consistency.
	membersCh := make(chan []pid.PID, 1)
	s.submit(func() {
		membersCh <- s.state.getMembers(group)
	})

	var members []pid.PID
	select {
	case members = <-membersCh:
	case <-s.ctx.Done():
		return 0, ErrServiceStopped
	}

	// Send outside the event loop so we don't block the action queue.
	sent := s.sendToMembers(from, topic, payloads, members)
	return sent, nil
}

// BroadcastLocal sends a message to local members of a group only.
func (s *Service) BroadcastLocal(from pid.PID, group pgapi.Group, topic string, payloads payload.Payloads) (int, error) {
	// Snapshot members inside the event loop for consistency.
	membersCh := make(chan []pid.PID, 1)
	s.submit(func() {
		membersCh <- s.state.getLocalMembers(group)
	})

	var members []pid.PID
	select {
	case members = <-membersCh:
	case <-s.ctx.Done():
		return 0, ErrServiceStopped
	}

	// Send outside the event loop so we don't block the action queue.
	sent := s.sendToMembers(from, topic, payloads, members)
	return sent, nil
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

	// Deliver to group monitors with circuit breaker protection
	s.deliverMonitorEventWithCircuitBreaker(group, pgapi.MemberJoined, pids)
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

	// Deliver to group monitors with circuit breaker protection
	s.deliverMonitorEventWithCircuitBreaker(group, pgapi.MemberLeft, pids)
}

// --- resource.Provider implementation ---

// Acquire returns a resource handle for this PG scope.
// The returned resource's Get() yields the ProcessGroups interface.
func (s *Service) Acquire(_ context.Context, _ registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	if mode != resource.ModeNormal {
		return nil, resource.ErrReleased
	}
	// Check if service is running by testing the snapshot
	if s.snap.Load() == nil {
		return nil, ErrServiceStopped
	}
	return &pgResource{svc: s}, nil
}

// pgResource wraps a Service as a resource.Resource[any].
type pgResource struct {
	svc    *Service
	closed bool
	mu     sync.Mutex
}

// Get returns the ScopeService interface for this scope.
func (r *pgResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrReleased
	}

	return pgapi.ScopeService(r.svc), nil
}

// Release frees the resource handle.
func (r *pgResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	r.closed = true
}

// Verify interface compliance.
var _ pgapi.ProcessGroups = (*Service)(nil)
var _ pgapi.ScopeService = (*Service)(nil)
var _ relay.Receiver = (*Service)(nil)
var _ resource.Provider = (*Service)(nil)
var _ supervisor.Service = (*Service)(nil)

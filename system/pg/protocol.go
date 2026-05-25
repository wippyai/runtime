// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"errors"
	"math/rand/v2"

	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// pickInitialDiscoverTargets returns up to `cap` randomly-chosen entries
// from `peers`. Used at service startup to bound the initial discover
// fan-out — peers we skip will discover us via gossip-delivered
// NodeJoined events instead.
func pickInitialDiscoverTargets(peers []pid.NodeID, cap int) []pid.NodeID {
	if len(peers) <= cap {
		out := make([]pid.NodeID, len(peers))
		copy(out, peers)
		return out
	}
	// Fisher-Yates partial shuffle: pick `cap` distinct random indices.
	idx := make([]int, len(peers))
	for i := range idx {
		idx[i] = i
	}
	for i := 0; i < cap; i++ {
		// G404: math/rand/v2 is fine here — we're picking random peers
		// for fan-out load distribution, not anything security-sensitive.
		j := i + rand.IntN(len(idx)-i) //nolint:gosec
		idx[i], idx[j] = idx[j], idx[i]
	}
	out := make([]pid.NodeID, cap)
	for i := 0; i < cap; i++ {
		out[i] = peers[idx[i]]
	}
	return out
}

// servicePID returns the service address for this PG scope on a given node.
// Uses empty UniqID — relay routes host-level receivers by Host alone.
func (s *Service) servicePID(nodeID pid.NodeID) pid.PID {
	return pid.PID{
		Node: nodeID,
		Host: s.hostID,
	}
}

// sendDiscover sends a discover message to a remote pg service.
// Uses circuit breaker to protect against slow nodes.
func (s *Service) sendDiscover(targetNodeID pid.NodeID) {
	// Check circuit breaker
	cb := s.cbManager.GetCircuitBreaker(targetNodeID)
	if !cb.Allow() {
		s.logger.Debug("circuit breaker open, skipping discover",
			logNodeID(targetNodeID),
		)
		return
	}

	pkg := relay.NewServicePackage(s.localNodeID, s.hostID, targetNodeID, s.hostID, pgapi.TopicDiscover,
		payload.New(map[string]any{
			"from": s.localNodeID,
		}),
	)
	if err := s.router.Send(pkg); err != nil {
		s.logger.Warn("failed to send discover",
			logNodeID(targetNodeID),
			logError(err),
		)
		cb.RecordFailure()

		// Add to retry queue if configured
		if s.retryQueue != nil && s.maxRetries > 0 {
			// Discover has no group/pids, use empty
			s.retryQueue.Add(targetNodeID, pgapi.TopicDiscover, nil, nil, nil)
		}
		return
	}

	cb.RecordSuccess()
}

// sendSync sends a full state sync to a remote pg service.
// Uses circuit breaker to protect against slow nodes.
func (s *Service) sendSync(targetNodeID pid.NodeID) {
	// Check circuit breaker
	cb := s.cbManager.GetCircuitBreaker(targetNodeID)
	if !cb.Allow() {
		s.logger.Debug("circuit breaker open, skipping sync",
			logNodeID(targetNodeID),
		)
		return
	}

	localPids := s.state.allLocalPids()

	// Convert pid.PID to interface types that match JSON deserialization
	// format (map[string]any with []any values). This ensures handlers
	// work identically whether payloads pass through codec serialization
	// or are forwarded directly in tests.
	groups := make(map[string]any, len(localPids))
	for group, pids := range localPids {
		strs := make([]any, len(pids))
		for i, p := range pids {
			strs[i] = p.String()
		}
		groups[group] = strs
	}

	pkg := relay.NewServicePackage(s.localNodeID, s.hostID, targetNodeID, s.hostID, pgapi.TopicSync,
		payload.New(map[string]any{
			"from":   s.localNodeID,
			"groups": groups,
		}),
	)
	if err := s.router.Send(pkg); err != nil {
		s.logger.Warn("failed to send sync",
			logNodeID(targetNodeID),
			logError(err),
		)
		cb.RecordFailure()
		return
	}

	cb.RecordSuccess()
}

// encodeJoinsPayload converts a (group -> pids) map into the wire format
// once, so it can be shared across every remote node in a fan-out.
func encodeJoinsPayload(joins map[string][]pid.PID) map[string][]string {
	wire := make(map[string][]string, len(joins))
	for g, pids := range joins {
		strs := make([]string, len(pids))
		for i, p := range pids {
			strs[i] = p.String()
		}
		wire[g] = strs
	}
	return wire
}

// retryJoinsPerGroup spills a batch broadcast back into the retry queue using
// the per-group entry shape the queue expects.
func (s *Service) retryJoinsPerGroup(nodeID pid.NodeID, topic string, joins map[string][]pid.PID) {
	if s.retryQueue == nil {
		return
	}
	for g, pids := range joins {
		s.retryQueue.Add(nodeID, topic, []string{g}, pids, nil)
	}
}

// broadcastJoin sends a batch join notification to all known remote pg
// services. One packet per remote carries every (group, pids) entry in the
// caller's map; single-group Join uses a 1-entry map so the fast path stays
// trivial. Uses circuit breaker for per-node protection and retry queue for
// recovery.
func (s *Service) broadcastJoin(joins map[string][]pid.PID) {
	if len(joins) == 0 || len(s.state.remote) == 0 {
		return
	}

	wire := encodeJoinsPayload(joins)

	for nodeID := range s.state.remote {
		if nodeID == s.localNodeID {
			continue
		}

		cb := s.cbManager.GetCircuitBreaker(nodeID)
		if !cb.Allow() {
			s.logger.Debug("circuit breaker open, skipping join broadcast",
				logNodeID(nodeID),
				zap.Int("groups", len(joins)),
			)
			s.retryJoinsPerGroup(nodeID, pgapi.TopicJoin, joins)
			continue
		}

		pkg := relay.NewServicePackage(s.localNodeID, s.hostID, nodeID, s.hostID, pgapi.TopicJoin,
			payload.New(map[string]any{
				"from":  s.localNodeID,
				"joins": wire,
			}),
		)
		if err := s.router.Send(pkg); err != nil {
			s.logger.Warn("failed to broadcast join",
				logNodeID(nodeID),
				logError(err),
			)
			cb.RecordFailure()
			s.retryJoinsPerGroup(nodeID, pgapi.TopicJoin, joins)
			continue
		}

		cb.RecordSuccess()
	}
}

// broadcastLeave is the leave counterpart of broadcastJoin: one packet per
// remote carries every (group, pids) entry. Multi-join semantics are
// preserved by repeating the PID in the value list. Groups whose value
// list is empty are dropped; if every group is empty the whole call is
// a no-op (matches the pre-batch guard).
func (s *Service) broadcastLeave(leaves map[string][]pid.PID) {
	if len(s.state.remote) == 0 {
		return
	}
	filtered := make(map[string][]pid.PID, len(leaves))
	for g, pids := range leaves {
		if len(pids) > 0 {
			filtered[g] = pids
		}
	}
	if len(filtered) == 0 {
		return
	}
	leaves = filtered

	wire := encodeJoinsPayload(leaves)

	for nodeID := range s.state.remote {
		if nodeID == s.localNodeID {
			continue
		}

		cb := s.cbManager.GetCircuitBreaker(nodeID)
		if !cb.Allow() {
			s.logger.Debug("circuit breaker open, skipping leave broadcast",
				logNodeID(nodeID),
				zap.Int("groups", len(leaves)),
			)
			s.retryJoinsPerGroup(nodeID, pgapi.TopicLeave, leaves)
			continue
		}

		pkg := relay.NewServicePackage(s.localNodeID, s.hostID, nodeID, s.hostID, pgapi.TopicLeave,
			payload.New(map[string]any{
				"from":   s.localNodeID,
				"leaves": wire,
			}),
		)
		if err := s.router.Send(pkg); err != nil {
			s.logger.Warn("failed to broadcast leave",
				logNodeID(nodeID),
				logError(err),
			)
			cb.RecordFailure()
			s.retryJoinsPerGroup(nodeID, pgapi.TopicLeave, leaves)
			continue
		}

		cb.RecordSuccess()
	}
}

// handleDiscover processes a discover message from a remote node.
func (s *Service) handleDiscover(fromNodeID pid.NodeID) {
	// Ignore self-discovery
	if fromNodeID == s.localNodeID {
		return
	}

	// Send our local state to the remote node
	s.sendSync(fromNodeID)

	// If we don't know about this node yet, discover it back
	if _, exists := s.state.remote[fromNodeID]; !exists {
		// Register the remote node (empty state for now, will be filled by sync)
		s.state.remote[fromNodeID] = &remoteNode{
			nodeID: fromNodeID,
			groups: make(map[string][]pid.PID),
		}
		s.sendDiscover(fromNodeID)
	}
}

// handleSync processes a sync message from a remote node.
// Uses differential sync (like Erlang PG) to avoid spurious events:
// only PIDs actually added or removed emit join/leave notifications.
func (s *Service) handleSync(fromNodeID pid.NodeID, groups map[string][]pid.PID) {
	// Capture old state before sync replaces it
	oldRN, oldExists := s.state.remote[fromNodeID]
	oldGroups := make(map[string][]pid.PID)
	if oldExists {
		for g, pids := range oldRN.groups {
			cp := make([]pid.PID, len(pids))
			copy(cp, pids)
			oldGroups[g] = cp
		}
	}

	s.state.syncRemote(fromNodeID, groups)

	// Differential event emission:
	// 1. For groups in old state: emit leave for removed PIDs
	for group, oldPids := range oldGroups {
		newPids := groups[group] // nil if group removed entirely
		removed := diffPIDs(oldPids, newPids)
		if len(removed) > 0 {
			s.emitLeaveEvent(group, removed)
		}
	}
	// 2. For groups in new state: emit join for added PIDs
	for group, newPids := range groups {
		oldPids := oldGroups[group] // nil if group is new
		added := diffPIDs(newPids, oldPids)
		if len(added) > 0 {
			s.emitJoinEvent(group, added)
		}
	}

	s.logger.Debug("synced remote state",
		logNodeID(fromNodeID),
		logGroupCount(len(groups)),
	)
}

// diffPIDs returns PIDs present in `a` but not in `b`, respecting
// multiplicity (like Erlang's lists:subtract / `--` operator).
// If a PID appears 3 times in `a` and 1 time in `b`, it appears 2 times
// in the result.
func diffPIDs(a, b []pid.PID) []pid.PID {
	if len(a) == 0 {
		return nil
	}
	if len(b) == 0 {
		result := make([]pid.PID, len(a))
		copy(result, a)
		return result
	}

	// Count occurrences in b
	bCounts := make(map[string]int, len(b))
	for _, p := range b {
		bCounts[p.String()]++
	}

	// Collect PIDs from a that exceed b's count
	var result []pid.PID
	for _, p := range a {
		key := p.String()
		if bCounts[key] > 0 {
			bCounts[key]--
		} else {
			result = append(result, p)
		}
	}
	return result
}

// handleRemoteJoin processes a join message from a remote node.
func (s *Service) handleRemoteJoin(fromNodeID pid.NodeID, group string, pids []pid.PID) {
	s.state.joinRemote(fromNodeID, group, pids)

	// Emit membership event for remote joins
	s.emitJoinEvent(group, pids)
}

// handleRemoteLeave processes a leave message from a remote node.
func (s *Service) handleRemoteLeave(fromNodeID pid.NodeID, pids []pid.PID, groups []string) {
	removed := s.state.leaveRemote(fromNodeID, pids, groups)

	// Emit membership events only for PIDs that were actually removed from each group.
	for group, removedPIDs := range removed {
		s.emitLeaveEvent(group, removedPIDs)
	}
}

// monitorProcess starts monitoring a local process via topology.
func (s *Service) monitorProcess(p pid.PID) {
	self := s.servicePID(s.localNodeID)
	if err := s.topo.Monitor(self, p); err != nil {
		// Ignore already monitoring errors (multi-join)
		if !errors.Is(err, topology.ErrAlreadyMonitoring) {
			s.logger.Warn("failed to monitor process",
				logPID(p),
				logError(err),
			)
		}
	}
}

// demonitorProcess stops monitoring a local process via topology.
func (s *Service) demonitorProcess(p pid.PID) {
	self := s.servicePID(s.localNodeID)
	_ = s.topo.Demonitor(self, p)
}

// handleProcessExit handles a local process exit.
func (s *Service) handleProcessExit(p pid.PID) {
	groups := s.state.leaveAllLocal(p)
	if len(groups) > 0 {
		// Broadcast the full list (with duplicates) to remote nodes so they
		// remove the correct number of occurrences for multi-join semantics.
		// The PID is repeated in each group's value list once per join, so
		// the receiver's leaveRemote removes the matching number of slots.
		leaves := make(map[string][]pid.PID, len(groups))
		for _, g := range groups {
			leaves[g] = append(leaves[g], p)
		}
		s.broadcastLeave(leaves)

		// Emit membership events per unique group.
		// Erlang PG sends one leave event per group with the PID repeated
		// for each join occurrence removed. E.g. if P joined "A" 3 times,
		// monitors receive leave("A", [P, P, P]).
		groupCounts := make(map[string]int, len(groups))
		for _, g := range groups {
			groupCounts[g]++
		}
		for group, count := range groupCounts {
			pids := make([]pid.PID, count)
			for i := range count {
				pids[i] = p
			}
			s.emitLeaveEvent(group, pids)
		}

		s.logger.Debug("process exited, removed from groups",
			logPID(p),
			logGroupCount(len(groupCounts)),
		)
	}

	// Clean up any monitor subscriptions owned by the exited process.
	// This mirrors Erlang PG's automatic monitor cleanup on process death.
	s.removeMonitorsByPID(p)
}

// handleNodeLeft handles a remote node leaving the cluster.
func (s *Service) handleNodeLeft(nodeID pid.NodeID) {
	// Collect groups and PIDs before removing, so we can emit events after state removal
	type leaveEvent struct {
		group string
		pids  []pid.PID
	}
	var events []leaveEvent

	rn, exists := s.state.remote[nodeID]
	if exists {
		for group, pids := range rn.groups {
			if len(pids) > 0 {
				pidsCopy := make([]pid.PID, len(pids))
				copy(pidsCopy, pids)
				events = append(events, leaveEvent{group: group, pids: pidsCopy})
			}
		}
	}

	s.state.removeNode(nodeID)

	// Clean up circuit breaker for departed node
	s.cbManager.RemoveCircuitBreaker(nodeID)

	// Evict monitor subscriptions owned by PIDs on the departed node.
	// Without this, the s.monitors map leaks indefinitely whenever a node
	// goes away without each of its PIDs cleanly demonitoring — the common
	// case under partition / pod kill chaos.
	if evicted := s.removeMonitorsByNode(nodeID); evicted > 0 {
		s.tel.recordMonitorsEvicted("node_left", evicted)
	}

	// Emit leave events after state removal
	for _, e := range events {
		s.emitLeaveEvent(e.group, e.pids)
	}

	s.logger.Debug("node left, removed remote state",
		logNodeID(nodeID),
	)
}

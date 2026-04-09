// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"errors"

	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
)

// pgPID returns the synthetic PID for the pg service on a given node.
func pgPID(nodeID pid.NodeID) pid.PID {
	return pid.PID{
		Node:   nodeID,
		Host:   pgapi.HostID,
		UniqID: "pg",
	}
}

// sendDiscover sends a discover message to a remote pg service.
func (s *Service) sendDiscover(targetNodeID pid.NodeID) {
	target := pgPID(targetNodeID)
	source := pgPID(s.localNodeID)

	pkg := relay.NewPackage(source, target, pgapi.TopicDiscover,
		payload.New(map[string]any{
			"from": s.localNodeID,
		}),
	)
	if err := s.router.Send(pkg); err != nil {
		s.logger.Warn("failed to send discover",
			logNodeID(targetNodeID),
			logError(err),
		)
	}
}

// sendSync sends a full state sync to a remote pg service.
func (s *Service) sendSync(targetNodeID pid.NodeID) {
	target := pgPID(targetNodeID)
	source := pgPID(s.localNodeID)

	localPids := s.state.allLocalPids()

	// Convert pid.PID to string representation for serialization
	groups := make(map[string][]string, len(localPids))
	for group, pids := range localPids {
		strs := make([]string, len(pids))
		for i, p := range pids {
			strs[i] = p.String()
		}
		groups[group] = strs
	}

	pkg := relay.NewPackage(source, target, pgapi.TopicSync,
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
	}
}

// broadcastJoin sends a join notification to all known remote pg services.
func (s *Service) broadcastJoin(group string, pids []pid.PID) {
	source := pgPID(s.localNodeID)

	pidStrs := make([]string, len(pids))
	for i, p := range pids {
		pidStrs[i] = p.String()
	}

	for nodeID := range s.state.remote {
		target := pgPID(nodeID)
		pkg := relay.NewPackage(source, target, pgapi.TopicJoin,
			payload.New(map[string]any{
				"from":  s.localNodeID,
				"group": group,
				"pids":  pidStrs,
			}),
		)
		if err := s.router.Send(pkg); err != nil {
			s.logger.Warn("failed to broadcast join",
				logNodeID(nodeID),
				logError(err),
			)
		}
	}
}

// broadcastLeave sends a leave notification to all known remote pg services.
func (s *Service) broadcastLeave(pids []pid.PID, groups []string) {
	if len(pids) == 0 || len(groups) == 0 {
		return
	}

	source := pgPID(s.localNodeID)

	pidStrs := make([]string, len(pids))
	for i, p := range pids {
		pidStrs[i] = p.String()
	}

	for nodeID := range s.state.remote {
		target := pgPID(nodeID)
		pkg := relay.NewPackage(source, target, pgapi.TopicLeave,
			payload.New(map[string]any{
				"from":   s.localNodeID,
				"pids":   pidStrs,
				"groups": groups,
			}),
		)
		if err := s.router.Send(pkg); err != nil {
			s.logger.Warn("failed to broadcast leave",
				logNodeID(nodeID),
				logError(err),
			)
		}
	}
}

// handleDiscover processes a discover message from a remote node.
func (s *Service) handleDiscover(fromNodeID pid.NodeID) {
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
	self := pgPID(s.localNodeID)
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
	self := pgPID(s.localNodeID)
	_ = s.topo.Demonitor(self, p)
}

// handleProcessExit handles a local process exit.
func (s *Service) handleProcessExit(p pid.PID) {
	groups := s.state.leaveAllLocal(p)
	if len(groups) > 0 {
		// Broadcast the full list (with duplicates) to remote nodes so they
		// remove the correct number of occurrences for multi-join semantics.
		s.broadcastLeave([]pid.PID{p}, groups)

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
	// Collect groups and PIDs before removing, so we can emit events
	rn, exists := s.state.remote[nodeID]
	if exists {
		for group, pids := range rn.groups {
			if len(pids) > 0 {
				// Make a copy of the pids slice before state removal
				pidsCopy := make([]pid.PID, len(pids))
				copy(pidsCopy, pids)
				// Defer emission until after state removal
				defer s.emitLeaveEvent(group, pidsCopy)
			}
		}
	}

	s.state.removeNode(nodeID)
	s.logger.Debug("node left, removed remote state",
		logNodeID(nodeID),
	)
}

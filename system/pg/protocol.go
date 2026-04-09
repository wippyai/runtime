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
func (s *Service) handleSync(fromNodeID pid.NodeID, groups map[string][]pid.PID) {
	// Collect old state for leave events before sync replaces it
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

	// Emit leave events for PIDs that were removed by the sync
	for group, oldPids := range oldGroups {
		if _, stillExists := groups[group]; !stillExists {
			s.emitLeaveEvent(group, oldPids)
		}
	}

	// Emit join events for new groups/PIDs from the sync
	for group, pids := range groups {
		if len(pids) > 0 {
			s.emitJoinEvent(group, pids)
		}
	}

	s.logger.Debug("synced remote state",
		logNodeID(fromNodeID),
		logGroupCount(len(groups)),
	)
}

// handleRemoteJoin processes a join message from a remote node.
func (s *Service) handleRemoteJoin(fromNodeID pid.NodeID, group string, pids []pid.PID) {
	s.state.joinRemote(fromNodeID, group, pids)

	// Emit membership event for remote joins
	s.emitJoinEvent(group, pids)
}

// handleRemoteLeave processes a leave message from a remote node.
func (s *Service) handleRemoteLeave(fromNodeID pid.NodeID, pids []pid.PID, groups []string) {
	s.state.leaveRemote(fromNodeID, pids, groups)

	// Emit membership events for remote leaves
	for _, group := range groups {
		s.emitLeaveEvent(group, pids)
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

		// Emit membership events per unique group (deduplicated)
		seen := make(map[string]bool, len(groups))
		for _, group := range groups {
			if !seen[group] {
				seen[group] = true
				s.emitLeaveEvent(group, []pid.PID{p})
			}
		}

		s.logger.Debug("process exited, removed from groups",
			logPID(p),
			logGroupCount(len(seen)),
		)
	}
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

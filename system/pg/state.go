// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"github.com/wippyai/runtime/api/pid"
)

// groupSnapshot is an immutable snapshot of a single group's membership.
// Used for lock-free reads via atomic.Pointer.
type groupSnapshot struct {
	all   []pid.PID
	local []pid.PID
}

// stateSnapshot is an immutable snapshot of all group memberships.
// Published via atomic.Pointer after each mutation.
type stateSnapshot struct {
	groups map[string]*groupSnapshot
}

// copyPIDs creates a copy of a PID slice.
func copyPIDs(pids []pid.PID) []pid.PID {
	if len(pids) == 0 {
		return nil
	}
	result := make([]pid.PID, len(pids))
	copy(result, pids)
	return result
}

// buildSnapshot creates an immutable snapshot from the current mutable state.
func (s *state) buildSnapshot() *stateSnapshot {
	snap := &stateSnapshot{
		groups: make(map[string]*groupSnapshot, len(s.groups)),
	}
	for g, gs := range s.groups {
		if len(gs.all) > 0 {
			snap.groups[g] = &groupSnapshot{
				all:   copyPIDs(gs.all),
				local: copyPIDs(gs.local),
			}
		}
	}
	return snap
}

// localProcess tracks a local process and which groups it has joined.
type localProcess struct {
	pid    pid.PID
	groups []string // groups joined (may contain duplicates for multi-join)
}

// remoteNode tracks group memberships from a single remote node.
type remoteNode struct {
	groups map[string][]pid.PID // group -> list of PIDs from this node
	nodeID pid.NodeID
}

// groupState holds the membership state for a single group.
type groupState struct {
	all   []pid.PID // all members (local + remote)
	local []pid.PID // local members only
}

// state is the internal membership state, accessed only from the event loop goroutine.
type state struct {
	local  map[string]*localProcess // pid.String() -> process info
	remote map[string]*remoteNode   // nodeID -> remote node state
	groups map[string]*groupState   // group name -> group state
}

func newState() *state {
	return &state{
		local:  make(map[string]*localProcess),
		remote: make(map[string]*remoteNode),
		groups: make(map[string]*groupState),
	}
}

// joinLocal adds a local process to a group.
func (s *state) joinLocal(group string, p pid.PID) {
	key := p.String()

	// Update local process tracking
	lp, exists := s.local[key]
	if !exists {
		lp = &localProcess{pid: p}
		s.local[key] = lp
	}
	lp.groups = append(lp.groups, group)

	// Update group state
	gs, exists := s.groups[group]
	if !exists {
		gs = &groupState{}
		s.groups[group] = gs
	}
	gs.all = append(gs.all, p)
	gs.local = append(gs.local, p)
}

// leaveLocal removes a local process from a group.
// Returns false if the process wasn't in the group.
func (s *state) leaveLocal(group string, p pid.PID) bool {
	key := p.String()
	lp, exists := s.local[key]
	if !exists {
		return false
	}

	// Remove first occurrence of group from the process's groups list
	found := false
	for i, g := range lp.groups {
		if g == group {
			lp.groups = append(lp.groups[:i], lp.groups[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return false
	}

	// Clean up empty local process
	if len(lp.groups) == 0 {
		delete(s.local, key)
	}

	// Update group state
	s.removePIDFromGroup(group, p, true)
	return true
}

// leaveAllLocal removes a local process from all groups.
// Returns the list of groups the process was in.
func (s *state) leaveAllLocal(p pid.PID) []string {
	key := p.String()
	lp, exists := s.local[key]
	if !exists {
		return nil
	}

	// Deduplicate groups for broadcasting
	groupSet := make(map[string]struct{})
	for _, g := range lp.groups {
		groupSet[g] = struct{}{}
	}

	groups := make([]string, 0, len(groupSet))
	for g := range groupSet {
		groups = append(groups, g)
	}

	// Remove from each group (handle multi-join by counting occurrences)
	groupCounts := make(map[string]int)
	for _, g := range lp.groups {
		groupCounts[g]++
	}
	for g, count := range groupCounts {
		for range count {
			s.removePIDFromGroup(g, p, true)
		}
	}

	delete(s.local, key)
	return groups
}

// joinRemote adds remote PIDs to a group.
func (s *state) joinRemote(nodeID pid.NodeID, group string, pids []pid.PID) {
	rn, exists := s.remote[nodeID]
	if !exists {
		rn = &remoteNode{
			nodeID: nodeID,
			groups: make(map[string][]pid.PID),
		}
		s.remote[nodeID] = rn
	}

	rn.groups[group] = append(rn.groups[group], pids...)

	gs, exists := s.groups[group]
	if !exists {
		gs = &groupState{}
		s.groups[group] = gs
	}
	gs.all = append(gs.all, pids...)
}

// leaveRemote removes remote PIDs from groups.
// Each PID in the slice removes exactly one occurrence (preserving multi-join semantics).
func (s *state) leaveRemote(nodeID pid.NodeID, pids []pid.PID, groups []string) {
	rn, exists := s.remote[nodeID]
	if !exists {
		return
	}

	for _, group := range groups {
		for _, p := range pids {
			key := p.String()

			// Remove one occurrence from remote tracking
			if remotePids, ok := rn.groups[group]; ok {
				for i, rp := range remotePids {
					if rp.String() == key {
						rn.groups[group] = append(remotePids[:i], remotePids[i+1:]...)
						break
					}
				}
				if len(rn.groups[group]) == 0 {
					delete(rn.groups, group)
				}
			}

			// Remove one occurrence from group state
			s.removePIDFromGroup(group, p, false)
		}
	}

	// Clean up empty remote node
	if len(rn.groups) == 0 {
		delete(s.remote, nodeID)
	}
}

// removeNode removes all PIDs from a remote node.
func (s *state) removeNode(nodeID pid.NodeID) {
	rn, exists := s.remote[nodeID]
	if !exists {
		return
	}

	for group, pids := range rn.groups {
		for _, p := range pids {
			s.removePIDFromGroup(group, p, false)
		}
	}

	delete(s.remote, nodeID)
}

// syncRemote replaces all known state for a remote node with new data.
func (s *state) syncRemote(nodeID pid.NodeID, groups map[string][]pid.PID) {
	// Remove old state for this node
	s.removeNode(nodeID)

	// Add new state
	for group, pids := range groups {
		s.joinRemote(nodeID, group, pids)
	}
}

// getMembers returns all members of a group.
func (s *state) getMembers(group string) []pid.PID {
	gs, exists := s.groups[group]
	if !exists {
		return nil
	}
	// Return a copy to prevent external mutation
	result := make([]pid.PID, len(gs.all))
	copy(result, gs.all)
	return result
}

// getLocalMembers returns local members of a group.
func (s *state) getLocalMembers(group string) []pid.PID {
	gs, exists := s.groups[group]
	if !exists {
		return nil
	}
	result := make([]pid.PID, len(gs.local))
	copy(result, gs.local)
	return result
}

// whichGroups returns all groups that have at least one member.
func (s *state) whichGroups() []string {
	groups := make([]string, 0, len(s.groups))
	for g, gs := range s.groups {
		if len(gs.all) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// whichLocalGroups returns all groups that have at least one local member.
func (s *state) whichLocalGroups() []string {
	groups := make([]string, 0, len(s.groups))
	for g, gs := range s.groups {
		if len(gs.local) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// allLocalPids returns all local PIDs grouped by group name, for sync protocol.
func (s *state) allLocalPids() map[string][]pid.PID {
	result := make(map[string][]pid.PID)
	for _, lp := range s.local {
		for _, g := range lp.groups {
			result[g] = append(result[g], lp.pid)
		}
	}
	return result
}

// allGroupMembers returns a snapshot of all groups and their members.
func (s *state) allGroupMembers() map[string][]pid.PID {
	result := make(map[string][]pid.PID, len(s.groups))
	for g, gs := range s.groups {
		if len(gs.all) > 0 {
			members := make([]pid.PID, len(gs.all))
			copy(members, gs.all)
			result[g] = members
		}
	}
	return result
}

// removePIDFromGroup removes one occurrence of a PID from a group's member lists.
func (s *state) removePIDFromGroup(group string, p pid.PID, isLocal bool) {
	gs, exists := s.groups[group]
	if !exists {
		return
	}

	key := p.String()

	// Remove from all list (first occurrence)
	for i, mp := range gs.all {
		if mp.String() == key {
			gs.all = append(gs.all[:i], gs.all[i+1:]...)
			break
		}
	}

	// Remove from local list if applicable
	if isLocal {
		for i, mp := range gs.local {
			if mp.String() == key {
				gs.local = append(gs.local[:i], gs.local[i+1:]...)
				break
			}
		}
	}

	// Clean up empty group
	if len(gs.all) == 0 {
		delete(s.groups, group)
	}
}

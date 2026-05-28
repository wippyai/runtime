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

// copyPIDs creates a copy of a PID slice.
func copyPIDs(pids []pid.PID) []pid.PID {
	if len(pids) == 0 {
		return nil
	}
	result := make([]pid.PID, len(pids))
	copy(result, pids)
	return result
}

// snapshotGroup builds an immutable snapshot for a single group, or returns
// nil if the group has no members (signaling deletion).
func (s *state) snapshotGroup(group string) *groupSnapshot {
	gs, ok := s.groups[group]
	if !ok || len(gs.all) == 0 {
		return nil
	}
	return &groupSnapshot{
		all:   copyPIDs(gs.all),
		local: copyPIDs(gs.local),
	}
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
	dirty  map[string]bool          // groups touched in the current event-loop closure
}

func newState() *state {
	return &state{
		local:  make(map[string]*localProcess),
		remote: make(map[string]*remoteNode),
		groups: make(map[string]*groupState),
		dirty:  make(map[string]bool),
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
	s.dirty[group] = true
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
// Returns the full list of groups the process was in, preserving duplicates
// for multi-join semantics. When a process joined "A" twice, the returned
// slice contains "A" twice so that remote nodes can remove all occurrences.
func (s *state) leaveAllLocal(p pid.PID) []string {
	key := p.String()
	lp, exists := s.local[key]
	if !exists {
		return nil
	}

	// Copy the full groups list (preserving duplicates for protocol correctness)
	groups := make([]string, len(lp.groups))
	copy(groups, lp.groups)

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
	s.dirty[group] = true
}

// leaveRemote removes remote PIDs from groups.
// Each PID in the slice removes exactly one occurrence (preserving multi-join semantics).
// Returns a map of group -> PIDs that were actually removed, so callers can emit
// accurate events only for PIDs that were truly members of each group.
func (s *state) leaveRemote(nodeID pid.NodeID, pids []pid.PID, groups []string) map[string][]pid.PID {
	rn, exists := s.remote[nodeID]
	if !exists {
		return nil
	}

	removed := make(map[string][]pid.PID)

	for _, group := range groups {
		for _, p := range pids {
			key := p.String()

			// Remove one occurrence from remote tracking
			found := false
			if remotePids, ok := rn.groups[group]; ok {
				for i, rp := range remotePids {
					if rp.String() == key {
						rn.groups[group] = append(remotePids[:i], remotePids[i+1:]...)
						found = true
						break
					}
				}
				if len(rn.groups[group]) == 0 {
					delete(rn.groups, group)
				}
			}

			// Only remove from group state if the PID was actually in
			// this node's remote tracking for this group.
			if found {
				s.removePIDFromGroup(group, p, false)
				removed[group] = append(removed[group], p)
			}
		}
	}

	// Clean up empty remote node
	if len(rn.groups) == 0 {
		delete(s.remote, nodeID)
	}

	return removed
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
// Always preserves the remote node entry so the node remains "known" even
// if it currently has no group memberships (prevents discover loops).
func (s *state) syncRemote(nodeID pid.NodeID, groups map[string][]pid.PID) {
	// Remove old state for this node
	s.removeNode(nodeID)

	// Add new state
	for group, pids := range groups {
		s.joinRemote(nodeID, group, pids)
	}

	// Ensure the remote node entry exists even with empty groups,
	// so the node remains "known" and doesn't trigger re-discovery.
	if _, exists := s.remote[nodeID]; !exists {
		s.remote[nodeID] = &remoteNode{
			nodeID: nodeID,
			groups: make(map[string][]pid.PID),
		}
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

// pidEqual compares two PIDs by their identity fields (Node, Host, UniqID),
// ignoring the cached string representation.
func pidEqual(a, b pid.PID) bool {
	return a.Node == b.Node && a.Host == b.Host && a.UniqID == b.UniqID
}

// removePIDFromGroup removes one occurrence of a PID from a group's member lists.
func (s *state) removePIDFromGroup(group string, p pid.PID, isLocal bool) {
	gs, exists := s.groups[group]
	if !exists {
		return
	}

	// Remove from all list (first occurrence)
	for i, mp := range gs.all {
		if pidEqual(mp, p) {
			gs.all = append(gs.all[:i], gs.all[i+1:]...)
			break
		}
	}

	// Remove from local list if applicable
	if isLocal {
		for i, mp := range gs.local {
			if pidEqual(mp, p) {
				gs.local = append(gs.local[:i], gs.local[i+1:]...)
				break
			}
		}
	}

	s.dirty[group] = true

	// Clean up empty group
	if len(gs.all) == 0 {
		delete(s.groups, group)
	}
}

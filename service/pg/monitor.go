// SPDX-License-Identifier: MPL-2.0

package pg

import (
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
)

// Monitor/Events subscriptions and their teardown, plus pg membership event
// emission. Monitor/Events atomically subscribe and snapshot on the event
// loop so no join/leave interleaves between subscribe and snapshot.

// Monitor atomically subscribes to a group's membership events and returns
// the current members. Because both operations happen inside a single event
// loop action, no join/leave can interleave between the subscription setup
// and the membership snapshot.
func (s *Service) Monitor(group string, p pid.PID, topic string) pgapi.MonitorResult {
	done := make(chan pgapi.MonitorResult, 1)
	if !s.submit(func() {
		// Assign unique ID
		s.monitorIDSeq++
		id := s.monitorIDSeq

		entry := &monitorEntry{
			pid:   p,
			topic: topic,
			id:    id,
		}

		s.monitors[group] = append(s.monitors[group], entry)
		s.monitorPIDCounts[p.String()]++

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
			if !s.submit(func() {
				s.removeMonitor(group, id, p)
				// If the process has no more group memberships and no more
				// monitor subscriptions, stop monitoring it.
				if !s.hasMonitorSubscriptions(p) && !s.hasGroupMemberships(p) {
					s.demonitorProcess(p)
				}
				unsub <- struct{}{}
			}) {
				// Action queue full: the removal closure never ran, so
				// don't wait on unsub (it would block until service ctx
				// cancellation). The monitor entry is reaped later on the
				// process/node-cleanup path.
				return
			}
			select {
			case <-unsub:
			case <-s.currentCtx().Done():
			}
		}

		done <- pgapi.MonitorResult{Members: members, Unsubscribe: unsubscribe}
	}) {
		return pgapi.MonitorResult{}
	}

	select {
	case result := <-done:
		return result
	case <-s.currentCtx().Done():
		return pgapi.MonitorResult{}
	}
}

// removeMonitor removes a monitor entry by ID and decrements the PID count.
// Must be called from event loop.
func (s *Service) removeMonitor(group string, id uint64, p pid.PID) {
	entries := s.monitors[group]
	for i, e := range entries {
		if e.id == id {
			s.monitors[group] = append(entries[:i], entries[i+1:]...)
			key := p.String()
			if s.monitorPIDCounts[key] > 0 {
				s.monitorPIDCounts[key]--
				if s.monitorPIDCounts[key] == 0 {
					delete(s.monitorPIDCounts, key)
				}
			}
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
	delete(s.monitorPIDCounts, key)
}

// removeMonitorsByNode removes all monitor subscriptions owned by PIDs
// hosted on the departed node. Without this, monitor entries leak forever
// for any node that left without each of its PIDs explicitly demonitoring
// (the common case under partition / pod kill chaos). The PID-level
// cleanup in removeMonitorsByPID does not cover this because it requires
// knowing every owning PID; the node-level cleanup is the only one that
// can be triggered on the cluster.NodeLeft event alone.
//
// Returns the number of entries evicted, for telemetry.
// Must be called from the event loop.
func (s *Service) removeMonitorsByNode(nodeID pid.NodeID) int {
	if nodeID == "" {
		return 0
	}
	evicted := 0
	for group, entries := range s.monitors {
		var remaining []*monitorEntry
		for _, e := range entries {
			if e.pid.Node != nodeID {
				remaining = append(remaining, e)
				continue
			}
			evicted++
			key := e.pid.String()
			if s.monitorPIDCounts[key] > 0 {
				s.monitorPIDCounts[key]--
				if s.monitorPIDCounts[key] == 0 {
					delete(s.monitorPIDCounts, key)
				}
			}
		}
		if len(remaining) == 0 {
			delete(s.monitors, group)
		} else if len(remaining) != len(entries) {
			s.monitors[group] = remaining
		}
	}
	return evicted
}

// hasMonitorSubscriptions returns true if the given PID has any active
// monitor subscriptions (group-specific or wildcard). O(1) via reverse index.
// Must be called from the event loop.
func (s *Service) hasMonitorSubscriptions(p pid.PID) bool {
	return s.monitorPIDCounts[p.String()] > 0
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
	if !s.submit(func() {
		s.monitorIDSeq++
		id := s.monitorIDSeq

		entry := &monitorEntry{
			pid:   p,
			topic: topic,
			id:    id,
		}

		// Wildcard key "" matches all groups
		s.monitors[""] = append(s.monitors[""], entry)
		s.monitorPIDCounts[p.String()]++

		// Monitor the subscriber process so we can clean up if it dies.
		s.monitorProcess(p)

		// Snapshot all current groups
		groups := s.state.allGroupMembers()

		unsubscribe := func() {
			unsub := make(chan struct{}, 1)
			if !s.submit(func() {
				s.removeMonitor("", id, p)
				if !s.hasMonitorSubscriptions(p) && !s.hasGroupMemberships(p) {
					s.demonitorProcess(p)
				}
				unsub <- struct{}{}
			}) {
				// Action queue full: the removal closure never ran, so
				// don't wait on unsub (it would block until service ctx
				// cancellation). The monitor entry is reaped later on the
				// process/node-cleanup path.
				return
			}
			select {
			case <-unsub:
			case <-s.currentCtx().Done():
			}
		}

		done <- pgapi.EventsResult{Groups: groups, Unsubscribe: unsubscribe}
	}) {
		return pgapi.EventsResult{}
	}

	select {
	case result := <-done:
		return result
	case <-s.currentCtx().Done():
		return pgapi.EventsResult{}
	}
}

// emitJoinEvent delivers a membership join event to group monitors via the relay.
func (s *Service) emitJoinEvent(group string, pids []pid.PID) {
	s.deliverMonitorEventWithCircuitBreaker(group, pgapi.MemberJoined, pids)
}

// emitLeaveEvent delivers a membership leave event to group monitors via the relay.
func (s *Service) emitLeaveEvent(group string, pids []pid.PID) {
	s.deliverMonitorEventWithCircuitBreaker(group, pgapi.MemberLeft, pids)
}

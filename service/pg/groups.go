// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"time"

	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ProcessGroups membership operations: join/leave (single and batch) and the
// group membership queries. Mutations run on the single-writer event loop;
// reads serve lock-free group snapshots.

// Join adds a local process to a group.
func (s *Service) Join(group pgapi.Group, p pid.PID) error {
	span := noopSpan
	if s.tel.tracing {
		_, span = s.tel.tracer.Start(s.currentCtx(), "pg.join",
			trace.WithAttributes(
				attribute.String("pg.name", group),
				attribute.String("node.id", s.localNodeID),
			),
		)
		defer span.End()
	}

	start := time.Now()
	done := acquireDoneChan()
	if !s.submit(func() {
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
		s.broadcastJoin(map[string][]pid.PID{group: {p}})

		// Emit membership event
		s.emitJoinEvent(group, []pid.PID{p})

		s.publishDirty()
		done <- nil
	}) {
		releaseDoneChan(done)
		err := s.submitError()
		s.tel.setSpanError(span, err)
		s.tel.recordJoin(group, err, time.Since(start))
		return err
	}

	select {
	case err := <-done:
		releaseDoneChan(done)
		s.tel.setSpanError(span, err)
		s.tel.recordJoin(group, err, time.Since(start))
		return err
	case <-s.currentCtx().Done():
		s.tel.setSpanError(span, ErrServiceStopped)
		s.tel.recordJoin(group, ErrServiceStopped, time.Since(start))
		return ErrServiceStopped
	}
}

// JoinGroups adds a local process to multiple groups atomically.
func (s *Service) JoinGroups(groups []pgapi.Group, p pid.PID) error {
	done := acquireDoneChan()
	if !s.submit(func() {
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

		joins := make(map[string][]pid.PID, len(groups))
		for _, group := range groups {
			s.state.joinLocal(group, p)
			joins[group] = append(joins[group], p)
			s.emitJoinEvent(group, []pid.PID{p})
		}
		s.broadcastJoin(joins)

		if !existed {
			s.monitorProcess(p)
		}

		s.publishDirty()
		done <- nil
	}) {
		releaseDoneChan(done)
		return s.submitError()
	}

	select {
	case err := <-done:
		releaseDoneChan(done)
		return err
	case <-s.currentCtx().Done():
		return ErrServiceStopped
	}
}

// Leave removes a local process from a group.
func (s *Service) Leave(group pgapi.Group, p pid.PID) error {
	span := noopSpan
	if s.tel.tracing {
		_, span = s.tel.tracer.Start(s.currentCtx(), "pg.leave",
			trace.WithAttributes(
				attribute.String("pg.name", group),
				attribute.String("node.id", s.localNodeID),
			),
		)
		defer span.End()
	}

	start := time.Now()
	done := acquireDoneChan()
	if !s.submit(func() {
		if !s.state.leaveLocal(group, p) {
			done <- ErrNotJoined
			return
		}

		// If the process has no more groups, stop monitoring
		if _, exists := s.state.local[p.String()]; !exists {
			s.demonitorProcess(p)
		}

		// Broadcast to remote nodes
		s.broadcastLeave(map[string][]pid.PID{group: {p}})

		// Emit membership event
		s.emitLeaveEvent(group, []pid.PID{p})

		s.publishDirty()
		done <- nil
	}) {
		releaseDoneChan(done)
		err := s.submitError()
		s.tel.setSpanError(span, err)
		s.tel.recordLeave(group, err, time.Since(start))
		return err
	}

	select {
	case err := <-done:
		releaseDoneChan(done)
		s.tel.setSpanError(span, err)
		s.tel.recordLeave(group, err, time.Since(start))
		return err
	case <-s.currentCtx().Done():
		s.tel.setSpanError(span, ErrServiceStopped)
		s.tel.recordLeave(group, ErrServiceStopped, time.Since(start))
		return ErrServiceStopped
	}
}

// LeaveGroups removes a local process from multiple groups.
// Following Erlang PG semantics: leaves all groups where the process is a member,
// skips groups where it isn't, and returns ErrNotJoined only if the process
// was not a member of ANY of the specified groups.
func (s *Service) LeaveGroups(groups []pgapi.Group, p pid.PID) error {
	done := acquireDoneChan()
	if !s.submit(func() {
		anyLeft := false
		leaves := make(map[string][]pid.PID, len(groups))
		for _, group := range groups {
			if s.state.leaveLocal(group, p) {
				anyLeft = true
				leaves[group] = append(leaves[group], p)
				s.emitLeaveEvent(group, []pid.PID{p})
			}
		}
		if anyLeft {
			s.broadcastLeave(leaves)
		}

		if !anyLeft {
			done <- ErrNotJoined
			return
		}

		// If the process has no more groups, stop monitoring
		if _, exists := s.state.local[p.String()]; !exists {
			s.demonitorProcess(p)
		}

		s.publishDirty()
		done <- nil
	}) {
		releaseDoneChan(done)
		return s.submitError()
	}

	select {
	case err := <-done:
		releaseDoneChan(done)
		return err
	case <-s.currentCtx().Done():
		return ErrServiceStopped
	}
}

// loadGroupSnap returns the immutable snapshot for a single group, or nil
// if the group is absent. O(1) amortized.
func (s *Service) loadGroupSnap(group pgapi.Group) *groupSnapshot {
	v, ok := s.groupSnaps.Load(group)
	if !ok {
		return nil
	}
	return v.(*groupSnapshot)
}

// GetMembers returns all members of a group across all nodes.
// Lock-free O(M_g) where M_g is the number of members in this group.
func (s *Service) GetMembers(group pgapi.Group) []pid.PID {
	gs := s.loadGroupSnap(group)
	if gs == nil {
		return nil
	}
	return copyPIDs(gs.all)
}

// GetLocalMembers returns local members of a group.
// Lock-free O(M_g_local) where M_g_local is the number of local members.
func (s *Service) GetLocalMembers(group pgapi.Group) []pid.PID {
	gs := s.loadGroupSnap(group)
	if gs == nil {
		return nil
	}
	return copyPIDs(gs.local)
}

// WhichGroups returns all groups that have at least one member.
// O(N) iteration over the per-group snapshot map; intended for
// discovery/debugging, not hot paths.
func (s *Service) WhichGroups() []pgapi.Group {
	var groups []pgapi.Group
	s.groupSnaps.Range(func(k, _ any) bool {
		groups = append(groups, k.(pgapi.Group))
		return true
	})
	return groups
}

// WhichLocalGroups returns groups that have at least one local member.
// O(N) iteration; cold path.
func (s *Service) WhichLocalGroups() []pgapi.Group {
	var groups []pgapi.Group
	s.groupSnaps.Range(func(k, v any) bool {
		if gs := v.(*groupSnapshot); len(gs.local) > 0 {
			groups = append(groups, k.(pgapi.Group))
		}
		return true
	})
	return groups
}

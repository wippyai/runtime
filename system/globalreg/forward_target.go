// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"errors"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
)

// maxForwardHops bounds the re-forward chain. A non-leader receiving a
// forwarded write at hop 0 re-forwards to its authoritative Leader() once
// (hop becomes 1); a recipient at hop >= 1 that is still not the leader
// errors out instead of forwarding again. The cap is the no-loop guarantee
// that keeps a momentary leader-flip from spinning a write around the
// cluster.
const maxForwardHops uint8 = 1

// MemberDeriver computes the ordered set of raft members a non-member should
// fall back to when no leader is known. The implementation closes over the
// deterministic selection pipeline + cluster-uniform caps so every node
// arrives at the same ordering for the same gossip snapshot.
//
// Boot wires this from system/raft.DeriveMembers + the configured
// MaxVoters/MaxStandbys so the globalreg package does not import the raft
// package directly. A nil deriver disables the derive-and-fan-out fallback;
// resolveForwardTarget then yields only the authoritative leader (if known).
type MemberDeriver func(nodes []cluster.NodeInfo) []cluster.NodeID

// SetMemberDeriver wires the deterministic raft-member derivation seam used
// by non-member forward paths. Safe to call before or after Start; callers
// may pass nil to disable derived-fallback (in which case forwards require a
// known leader).
func (s *Service) SetMemberDeriver(d MemberDeriver) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.memberDeriver = d
	s.mu.Unlock()
}

func (s *Service) loadMemberDeriver() MemberDeriver {
	s.mu.Lock()
	d := s.memberDeriver
	s.mu.Unlock()
	return d
}

// resolveForwardTarget returns the ordered list of node IDs a leader-directed
// write should try, in priority order:
//
//  1. The authoritative leader, if raftSvc.Leader() returns a non-empty ID.
//     A raft member observes leadership instantly via AppendEntries; a
//     non-member never observes it (no AE → no Leader()), so this branch
//     only fires for members and the historical 1-hop fast path is unchanged.
//  2. Otherwise (non-member, or member during an election window): the
//     deterministic raft-member set derived from the current membership
//     snapshot via the same pure pipeline reconcile uses. Lowest-ranked ID
//     first (the rank-order ID emitted by DeriveMembers).
//
// Self is excluded from the list; a Service that is itself the leader
// short-circuits via the direct Apply path before reaching this helper.
// Returns ErrNotAvailable when neither branch yields a candidate (no leader
// known AND derivation produced an empty set, e.g. an unwired deriver in a
// test that doesn't exercise non-member shape).
func (s *Service) resolveForwardTarget() ([]pid.NodeID, error) {
	out := make([]pid.NodeID, 0, 4)
	seen := make(map[pid.NodeID]struct{}, 4)
	add := func(id pid.NodeID) {
		if id == "" || id == s.localNode {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	if s.raftSvc != nil {
		if id, _, err := s.raftSvc.Leader(); err == nil {
			add(pid.NodeID(id))
		}
	}

	d := s.loadMemberDeriver()
	s.mu.Lock()
	mem := s.membership
	s.mu.Unlock()
	if d != nil && mem != nil {
		for _, id := range d(mem.Nodes()) {
			add(pid.NodeID(id))
		}
	}

	if len(out) == 0 {
		return nil, globalreg.ErrNotAvailable
	}
	return out, nil
}

// reForwardTarget computes the next-hop target a non-leader member should use
// when it receives a forwarded leader-directed message at the given hop count.
// Returns the authoritative leader ID and a true ok flag when the hop count is
// still under the cap AND raftSvc.Leader() returns a non-empty ID different
// from the local node. Returns ok=false when the cap has been hit (the
// message must error rather than loop), or when no leader is known.
//
// The cap is the no-loop guarantee: a forwarded write makes at most one
// re-forward hop. A second non-leader recipient stops, surfacing the failure
// to the original requester via the corr-id reply path.
func (s *Service) reForwardTarget(hop uint8) (pid.NodeID, bool) {
	if hop >= maxForwardHops {
		return "", false
	}
	if s.raftSvc == nil {
		return "", false
	}
	id, _, err := s.raftSvc.Leader()
	if err != nil || id == "" || pid.NodeID(id) == s.localNode {
		return "", false
	}
	return pid.NodeID(id), true
}

// errAllTargetsFailed is the aggregate error returned by trySendToTargets
// when every candidate target failed. Each individual cause is wrapped so
// the caller can inspect the chain.
type errAllTargetsFailed struct {
	causes []error
}

func (e *errAllTargetsFailed) Error() string {
	if len(e.causes) == 0 {
		return "forward: all targets failed"
	}
	return "forward: all targets failed: " + e.causes[len(e.causes)-1].Error()
}

func (e *errAllTargetsFailed) Unwrap() error {
	if len(e.causes) == 0 {
		return nil
	}
	return e.causes[len(e.causes)-1]
}

// joinErrors returns a single error capturing all attempt failures. Helps
// callers retain context across the candidate list.
func joinErrors(causes []error) error {
	switch len(causes) {
	case 0:
		return nil
	case 1:
		return causes[0]
	default:
		return &errAllTargetsFailed{causes: causes}
	}
}

// errNoTargetsAccepted is returned when the candidate list was non-empty but
// every send attempt failed. Distinguished from globalreg.ErrNotAvailable
// (no targets at all) so the caller can choose between retry and surface.
var errNoTargetsAccepted = errors.New("forward: candidates exhausted")

// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"net"
	"sort"
	"strconv"

	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/raft"
)

// candidate is an internal representation of a node considered for Raft
// voting. Pure data; carries pre-parsed gossip hints.
type candidate struct {
	ID            cluster.NodeID
	Addr          string // host:raft_port
	FailureDomain string
	Priority      int
}

// candidatesFromMembership turns a snapshot of cluster.Membership into the
// ordered candidate list used by reconcile. Nodes without a usable raft_port
// or with raft_eligible=false are filtered out.
//
// The returned slice is sorted deterministically: priority ascending, then ID
// ascending. Same input always yields same output, regardless of node arrival
// order in gossip.
func candidatesFromMembership(nodes []cluster.NodeInfo) []candidate {
	out := make([]candidate, 0, len(nodes))
	for _, n := range nodes {
		c, ok := candidateFromNode(n)
		if !ok {
			continue
		}
		out = append(out, c)
	}
	rankCandidates(out)
	return out
}

// candidateFromNode extracts a candidate from a single NodeInfo.
// Returns (zero, false) if the node is ineligible or lacks a valid raft_port.
func candidateFromNode(n cluster.NodeInfo) (candidate, bool) {
	if n.Meta == nil {
		return candidate{}, false
	}

	// raft_eligible defaults to true when missing, but explicit "false"
	// (or any non-true value other than empty) opts the node out.
	if v, ok := n.Meta["raft_eligible"]; ok && v != "" {
		eligible, err := strconv.ParseBool(v)
		if err != nil || !eligible {
			return candidate{}, false
		}
	}

	port := n.Meta["raft_port"]
	if !isValidPort(port) {
		return candidate{}, false
	}

	priority := 100 // default — must match boot/components/system/cluster.go
	if v, ok := n.Meta["raft_priority"]; ok && v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			priority = p
		}
	}

	host := n.Addr
	if h, _, err := net.SplitHostPort(n.Addr); err == nil {
		host = h
	}

	return candidate{
		ID:            n.ID,
		Addr:          joinHostPort(host, port),
		FailureDomain: n.Meta["failure_domain"],
		Priority:      priority,
	}, true
}

// rankCandidates sorts in place: lower priority first, ties broken by ID.
// Stable, deterministic across nodes — required so every leader makes the
// same selection from the same gossip view.
func rankCandidates(cs []candidate) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Priority != cs[j].Priority {
			return cs[i].Priority < cs[j].Priority
		}
		return cs[i].ID < cs[j].ID
	})
}

// desiredVoterCount returns the target voter count given the eligible pool
// size and the configured cap. The result is always odd (1, 3, 5, 7, 9...).
//
// Rules:
//   - 0 eligible → 0 voters
//   - 1 eligible → 1 voter
//   - 2 eligible → 1 voter (an even count cannot form a stable quorum)
//   - N>=3       → min(largest_odd<=N, maxVoters)
//
// maxVoters must be odd and >= 1; callers validate.
func desiredVoterCount(eligible, maxVoters int) int {
	if eligible <= 0 {
		return 0
	}
	if eligible == 1 {
		return 1
	}
	target := eligible
	if target%2 == 0 {
		target--
	}
	if target > maxVoters {
		target = maxVoters
	}
	if target < 1 {
		target = 1
	}
	return target
}

// pickVoters selects up to `target` candidates as voters, spreading across
// failure domains where possible. The current voter set is consulted to apply
// soft stickiness: a current voter that ranks within target+1 is preferred
// over a higher-ranked challenger to avoid churn under priority changes.
//
// Returns the selected voter IDs as a set. Inputs are not mutated.
func pickVoters(ranked []candidate, current map[cluster.NodeID]struct{}, target int) map[cluster.NodeID]struct{} {
	if target <= 0 || len(ranked) == 0 {
		return map[cluster.NodeID]struct{}{}
	}
	if target > len(ranked) {
		target = len(ranked)
	}

	// First pass: spread by failure domain. Walk ranked candidates and accept
	// the first one in each unseen domain until target is hit; then fall back
	// to plain rank order.
	picked := make(map[cluster.NodeID]struct{}, target)
	seenDomain := make(map[string]struct{})

	for _, c := range ranked {
		if len(picked) >= target {
			break
		}
		// Empty domain ("" — node didn't advertise) is treated as its own
		// bucket: any number of unlabeled nodes may be picked. This avoids
		// collapsing a homogeneous cluster to a single voter.
		if c.FailureDomain != "" {
			if _, dup := seenDomain[c.FailureDomain]; dup {
				continue
			}
			seenDomain[c.FailureDomain] = struct{}{}
		}
		picked[c.ID] = struct{}{}
	}

	// Fill remaining slots in plain rank order if spreading left us short.
	for _, c := range ranked {
		if len(picked) >= target {
			break
		}
		if _, dup := picked[c.ID]; dup {
			continue
		}
		picked[c.ID] = struct{}{}
	}

	// Stickiness: if a current voter ranks within target+1 but didn't make
	// the picked set (because spreading evicted it for a same-domain peer),
	// swap it back in displacing the lowest-ranked non-sticky pick. Bounded
	// to one swap per voter to keep the algorithm deterministic.
	if len(current) > 0 {
		applyStickiness(ranked, current, picked, target)
	}

	return picked
}

// applyStickiness mutates `picked` in place to bias toward keeping current
// voters that rank near the cut. Called only when current voter set is
// non-empty.
func applyStickiness(ranked []candidate, current, picked map[cluster.NodeID]struct{}, target int) {
	cutoff := target + 1
	if cutoff > len(ranked) {
		cutoff = len(ranked)
	}

	// Build rank→ID lookup for the top `cutoff` candidates.
	stickyEligible := make(map[cluster.NodeID]struct{}, cutoff)
	for i := 0; i < cutoff; i++ {
		if _, isCurrent := current[ranked[i].ID]; isCurrent {
			stickyEligible[ranked[i].ID] = struct{}{}
		}
	}

	// For each sticky-eligible current voter not already picked, find the
	// lowest-ranked picked node that is NOT a current voter and swap.
	for stickyID := range stickyEligible {
		if _, in := picked[stickyID]; in {
			continue
		}
		// Find lowest-rank non-sticky pick.
		var victimID cluster.NodeID
		victimRank := -1
		for i := len(ranked) - 1; i >= 0; i-- {
			id := ranked[i].ID
			if _, in := picked[id]; !in {
				continue
			}
			if _, isCurrent := current[id]; isCurrent {
				continue
			}
			victimID = id
			victimRank = i
			break
		}
		if victimRank < 0 {
			continue
		}
		delete(picked, victimID)
		picked[stickyID] = struct{}{}
	}
}

// reconcileDiff computes the set of Raft membership changes needed to move
// from `current` (live Raft config) toward `desired` (selected voters +
// remaining eligible nodes as nonvoters). Pure: mutates nothing.
//
// Returned ops are ordered for safe quorum-preserving execution:
//  1. promote/add voters first (grow quorum to absorb later removals)
//  2. demote voters → nonvoters
//  3. remove servers no longer eligible at all
type membershipOp struct {
	ID   cluster.NodeID
	Addr string
	Kind opKind
}

type opKind int

const (
	opAddVoter opKind = iota
	opAddNonvoter
	opPromote
	opDemote
	opRemove
)

// reconcileDiff plans Raft config changes.
//
//   - desiredVoters: nodes that must be voters
//   - allEligible:   superset (voters ∪ nonvoters); anything live but absent
//     here gets removed
//   - current:       current Raft config (from GetConfiguration)
//   - addrLookup:    nodeID → host:raft_port (for fresh adds)
func reconcileDiff(
	desiredVoters map[cluster.NodeID]struct{},
	allEligible []candidate,
	current []raftapi.Server,
	addrLookup map[cluster.NodeID]string,
) []membershipOp {
	// Index helpers.
	currentByID := make(map[string]raftapi.Server, len(current))
	for _, s := range current {
		currentByID[s.ID] = s
	}
	eligibleSet := make(map[cluster.NodeID]struct{}, len(allEligible))
	for _, c := range allEligible {
		eligibleSet[c.ID] = struct{}{}
	}

	var adds, demotes, removes []membershipOp

	// Pass 1: ensure every desired voter is a voter.
	for id := range desiredVoters {
		addr := addrLookup[id]
		srv, exists := currentByID[id]
		switch {
		case !exists:
			adds = append(adds, membershipOp{ID: id, Addr: addr, Kind: opAddVoter})
		case !srv.IsVoter:
			adds = append(adds, membershipOp{ID: id, Addr: addr, Kind: opPromote})
		case srv.Address != addr && addr != "":
			// Address drifted (peer rebound on different port). Re-add with
			// new address — hashicorp/raft treats AddVoter on existing ID as
			// an in-place address update.
			adds = append(adds, membershipOp{ID: id, Addr: addr, Kind: opAddVoter})
		}
	}

	// Pass 2: nonvoters for eligible-but-not-voter nodes.
	for _, c := range allEligible {
		if _, isVoter := desiredVoters[c.ID]; isVoter {
			continue
		}
		srv, exists := currentByID[c.ID]
		switch {
		case !exists:
			adds = append(adds, membershipOp{ID: c.ID, Addr: c.Addr, Kind: opAddNonvoter})
		case srv.IsVoter:
			demotes = append(demotes, membershipOp{ID: c.ID, Kind: opDemote})
		case srv.Address != c.Addr && c.Addr != "":
			adds = append(adds, membershipOp{ID: c.ID, Addr: c.Addr, Kind: opAddNonvoter})
		}
	}

	// Pass 3: remove anything live but no longer eligible.
	for _, s := range current {
		if _, ok := eligibleSet[s.ID]; ok {
			continue
		}
		removes = append(removes, membershipOp{ID: s.ID, Kind: opRemove})
	}

	// Stable order within each phase.
	sort.Slice(adds, func(i, j int) bool { return adds[i].ID < adds[j].ID })
	sort.Slice(demotes, func(i, j int) bool { return demotes[i].ID < demotes[j].ID })
	sort.Slice(removes, func(i, j int) bool { return removes[i].ID < removes[j].ID })

	out := make([]membershipOp, 0, len(adds)+len(demotes)+len(removes))
	out = append(out, adds...)
	out = append(out, demotes...)
	out = append(out, removes...)
	return out
}

// isValidPort returns true if s parses as an integer in [1, 65535].
func isValidPort(s string) bool {
	if s == "" {
		return false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n >= 1 && n <= 65535
}

// joinHostPort wraps net.JoinHostPort but takes a string port — convenient
// for callers that already have the gossip value as a string.
func joinHostPort(host, port string) string {
	// Avoid importing net here; selection.go stays pure stdlib-light.
	// net.JoinHostPort handles IPv6 brackets; we do the same minimal logic.
	if host == "" {
		return ":" + port
	}
	// IPv6 literal heuristic: contains ':' and is not already bracketed.
	if containsColon(host) && host[0] != '[' {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

func containsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}

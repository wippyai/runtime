// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"sort"
	"strconv"

	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

// candidate is an internal representation of a node considered for Raft
// voting. Pure data; carries pre-parsed gossip hints.
type candidate struct {
	ID            cluster.NodeID
	Addr          string // hraft.ServerAddress: the NodeID itself under internode RPC transport.
	FailureDomain string
	Priority      int
}

// PickForwardTarget chooses a raft member a registry non-member (role=client)
// forwards its kv ops to. It need not be the leader: the target member
// re-forwards to the leader it can resolve. nodes is a gossip snapshot
// (membership only reports live peers, so a departed target naturally drops out
// and the next call re-picks). self is excluded. Selection is deterministic
// (eligible-only, ranked by priority then ID) so retries are stable. Returns
// ("", false) when no other eligible member is visible yet.
func PickForwardTarget(nodes []cluster.NodeInfo, self cluster.NodeID) (cluster.NodeID, bool) {
	for _, c := range candidatesFromMembership(nodes) {
		if c.ID != self {
			return c.ID, true
		}
	}
	return "", false
}

// candidatesFromMembership turns a snapshot of cluster.Membership into the
// ordered candidate list used by reconcile. Nodes with raft_eligible=false
// are filtered out.
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
// Returns (zero, false) if the node is ineligible.
//
// Under the internode RPC transport, hraft.ServerAddress is the NodeID itself:
// there is no host:port to resolve.
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

	priority := 100 // default — must match boot/components/system/cluster.go
	if v, ok := n.Meta["raft_priority"]; ok && v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			priority = p
		}
	}

	return candidate{
		ID:            n.ID,
		Addr:          n.ID,
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
// size and the configured cap. Always odd for N>=3 (1, 3, 5, 7, 9...).
//
// Rules:
//   - 0 eligible → 0 voters
//   - 1 eligible → 1 voter
//   - 2 eligible → 2 voters (transient post-failure window; see note)
//   - N>=3       → min(largest_odd<=N, maxVoters)
//
// maxVoters must be odd and >= 1; callers validate.
//
// 2-eligible note: the historical rule rounded down to 1 voter on the
// grounds that an even quorum can split-vote. In practice 2-eligible
// arises overwhelmingly as a TRANSIENT window after a 3-voter cluster
// loses one node, and demoting the survivor causes a leadership
// transfer + churn cascade that breaks failover (run_chaos.sh fail
// mode). Keeping both as voters is no less available than 1-voter mode
// (both halt on one further failure) and strictly less churny — the
// reconciler restores 3-voter steady state as soon as a new node
// joins. Initial 2-node clusters should still be configured with
// MaxVoters=1 by the operator if they want 1-voter mode.
func desiredVoterCount(eligible, maxVoters int) int {
	if eligible <= 0 {
		return 0
	}
	if eligible == 1 {
		return 1
	}
	if eligible == 2 {
		if maxVoters < 2 {
			return maxVoters
		}
		return 2
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
	// Defensive clamp: never return an even count > 2 even if
	// applyDefaults was bypassed and maxVoters is even.
	if target > 2 && target%2 == 0 {
		target--
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
//
// Domain-aware victim selection: when swapping a sticky voter in, prefer a
// victim in the SAME failure domain as the sticky voter so the swap doesn't
// inadvertently concentrate voters in fewer domains. Only falls back to a
// lower-ranked cross-domain victim when no same-domain victim exists.
func applyStickiness(ranked []candidate, current, picked map[cluster.NodeID]struct{}, target int) {
	cutoff := target + 1
	if cutoff > len(ranked) {
		cutoff = len(ranked)
	}

	// Build rank→domain lookup so the swap step can compare domains.
	domainOf := make(map[cluster.NodeID]string, len(ranked))
	for _, c := range ranked {
		domainOf[c.ID] = c.FailureDomain
	}

	// Build set of current voters that rank within the cutoff (sticky-eligible).
	stickyEligible := make(map[cluster.NodeID]struct{}, cutoff)
	for i := 0; i < cutoff; i++ {
		if _, isCurrent := current[ranked[i].ID]; isCurrent {
			stickyEligible[ranked[i].ID] = struct{}{}
		}
	}

	// For each sticky-eligible current voter not already picked, find a victim:
	// preferring same-domain non-current picks (lowest-ranked first), falling
	// back to any non-current pick.
	for stickyID := range stickyEligible {
		if _, in := picked[stickyID]; in {
			continue
		}
		stickyDomain := domainOf[stickyID]

		victimID, ok := findStickinessVictim(ranked, picked, current, domainOf, stickyDomain, true)
		if !ok {
			victimID, ok = findStickinessVictim(ranked, picked, current, domainOf, stickyDomain, false)
		}
		if !ok {
			continue
		}
		delete(picked, victimID)
		picked[stickyID] = struct{}{}
	}
}

// findStickinessVictim walks `ranked` from lowest to highest rank looking for
// a picked node that is NOT a current voter. When `sameDomainOnly` is true,
// only candidates whose failure_domain matches `wantedDomain` qualify. Returns
// (id, true) on hit, (zero, false) on miss.
func findStickinessVictim(ranked []candidate, picked, current map[cluster.NodeID]struct{}, domainOf map[cluster.NodeID]string, wantedDomain string, sameDomainOnly bool) (cluster.NodeID, bool) {
	for i := len(ranked) - 1; i >= 0; i-- {
		id := ranked[i].ID
		if _, in := picked[id]; !in {
			continue
		}
		if _, isCurrent := current[id]; isCurrent {
			continue
		}
		if sameDomainOnly {
			if wantedDomain == "" || domainOf[id] != wantedDomain {
				continue
			}
		}
		return id, true
	}
	return "", false
}

// raftMembers returns the bounded set of nodes that belong in the Raft
// configuration: every picked voter plus up to maxStandbys highest-ranked
// non-voter candidates, kept as hot spares for fast voter promotion. Nodes
// outside this set are not Raft members at all, so the leader never fans
// AppendEntries out to them — this is what keeps idle leader CPU O(1) in
// cluster size rather than O(N). `ranked` must already be rank-ordered.
func raftMembers(ranked []candidate, voters map[cluster.NodeID]struct{}, maxStandbys int) []candidate {
	out := make([]candidate, 0, len(voters)+maxStandbys)
	standbys := 0
	for _, c := range ranked {
		if _, isVoter := voters[c.ID]; isVoter {
			out = append(out, c)
			continue
		}
		if standbys < maxStandbys {
			out = append(out, c)
			standbys++
		}
	}
	return out
}

// DeriveMembers computes the bounded raft membership (voters + standbys) a node
// should see for a given gossip snapshot and configured caps. Pure: same inputs
// always yield the same ordered output on every node, regardless of caller.
//
// The result is the set of node IDs in rank order (priority asc, then ID asc).
// Used by non-members to compute the candidate set they can forward writes to:
// raftMembers reads MaxVoters+MaxStandbys from cluster-uniform config, and the
// selection pipeline filters/ranks/picks deterministically — so a non-member
// arrives at the same membership decision the leader applies through reconcile.
//
// Caveat: the *actual* live Raft configuration can lag the derived set
// momentarily during voter ops, and stickiness in pickVoters can keep a
// current voter in place that ranking alone would evict. The forwarder uses
// the derived set as an ordered candidate list and falls back through it on
// send/timeout failure, so a transient mismatch resolves by retry.
func DeriveMembers(nodes []cluster.NodeInfo, maxVoters, maxStandbys int) []cluster.NodeID {
	if maxVoters <= 0 {
		maxVoters = defaultMaxVoters
	}
	if maxStandbys < 0 {
		maxStandbys = 0
	}
	ranked := candidatesFromMembership(nodes)
	target := desiredVoterCount(len(ranked), maxVoters)
	// Non-members have no view of the current voter set, so stickiness is
	// neutral here — passing nil yields the rank-order pick every node agrees
	// on for the same gossip snapshot.
	picked := pickVoters(ranked, nil, target)
	members := raftMembers(ranked, picked, maxStandbys)
	out := make([]cluster.NodeID, 0, len(members))
	for _, c := range members {
		out = append(out, c.ID)
	}
	return out
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
//   - desiredVoters:  nodes that must be voters
//   - desiredMembers: the bounded Raft membership (voters ∪ standby
//     nonvoters, from raftMembers); anything live but absent here gets removed
//   - current:        current Raft config (from GetConfiguration)
//   - addrLookup:     nodeID -> ServerAddress (the NodeID under internode RPC transport)
func reconcileDiff(
	desiredVoters map[cluster.NodeID]struct{},
	desiredMembers []candidate,
	current []raftapi.Server,
	addrLookup map[cluster.NodeID]string,
) []membershipOp {
	// Index helpers.
	currentByID := make(map[string]raftapi.Server, len(current))
	for _, s := range current {
		currentByID[s.ID] = s
	}
	memberSet := make(map[cluster.NodeID]struct{}, len(desiredMembers))
	for _, c := range desiredMembers {
		memberSet[c.ID] = struct{}{}
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

	// Pass 2: nonvoters for member-but-not-voter nodes (the standby pool).
	for _, c := range desiredMembers {
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

	// Pass 3: remove anything live but no longer a desired member — covers
	// both nodes gossip dropped and nodes pushed out of the bounded
	// membership by the standby cap.
	for _, s := range current {
		if _, ok := memberSet[s.ID]; ok {
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

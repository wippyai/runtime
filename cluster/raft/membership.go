// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/event"

	"go.uber.org/zap"
)

// HandlerConfig tunes the membership reconciler.
//
// Zero values are filled in by NewMembershipHandler with the defaults below.
// MaxVoters is rounded down to the nearest odd number; values <1 fall back
// to 5.
type HandlerConfig struct {
	// MaxVoters caps the voter set regardless of cluster size. Must be odd.
	// Default: 5.
	MaxVoters int

	// MaxStandbys caps how many non-voters are kept in the Raft configuration
	// as hot spares for voter promotion. Nodes beyond MaxVoters+MaxStandbys
	// are not Raft members at all, so the leader never fans AppendEntries out
	// to them — this is what keeps idle leader CPU O(1) in cluster size
	// instead of O(N). Default: 4.
	MaxStandbys int

	// ReconcileDebounce is the wait window after a gossip event before a
	// reconcile pass runs. Multiple events within the window coalesce.
	// Default: 2s.
	ReconcileDebounce time.Duration

	// ReconcileTimeout bounds a single reconcile pass. Each individual Raft
	// op gets a fraction of this. Default: 2s.
	ReconcileTimeout time.Duration
}

const (
	defaultMaxVoters         = 5
	defaultMaxStandbys       = 4
	defaultReconcileDebounce = 2 * time.Second
	defaultReconcileTimeout  = 2 * time.Second

	// busBuffer is the subscriber channel size. Sized generously so a
	// momentarily-busy reconcile loop does not back-pressure the global
	// event dispatcher (see system/eventbus/bus.go: blocking Send).
	busBuffer = 128
)

// applyDefaults fills in zero fields and clamps MaxVoters to a valid odd value.
// Returns the effective config used.
func (c HandlerConfig) applyDefaults(logger *zap.Logger) HandlerConfig {
	if c.MaxVoters <= 0 {
		c.MaxVoters = defaultMaxVoters
	}
	if c.MaxVoters%2 == 0 {
		// Even values cannot form a stable quorum — round down with a warning.
		logger.Warn("MaxVoters must be odd; rounding down",
			zap.Int("requested", c.MaxVoters),
			zap.Int("effective", c.MaxVoters-1),
		)
		c.MaxVoters--
		if c.MaxVoters < 1 {
			c.MaxVoters = 1
		}
	}
	if c.MaxStandbys <= 0 {
		c.MaxStandbys = defaultMaxStandbys
	}
	if c.ReconcileDebounce <= 0 {
		c.ReconcileDebounce = defaultReconcileDebounce
	}
	if c.ReconcileTimeout <= 0 {
		c.ReconcileTimeout = defaultReconcileTimeout
	}
	return c
}

// MembershipHandler reconciles the Raft voter set against cluster membership.
//
// Flow:
//   - Subscribe to NodeJoined/NodeLeft/NodeUpdated/LeaderElected on the bus.
//   - On any gossip event: schedule a debounced reconcile.
//   - On LeaderElected (this node won): reconcile immediately.
//   - Reconcile reads the current Raft config + membership snapshot, picks
//     voters via the rules in selection.go, and emits a minimal sequence of
//     promote → demote → remove ops.
//
// Only the leader executes ops; non-leaders silently noop. The IsLeader
// check is best-effort — if leadership transfers mid-reconcile, ops will
// return ErrNotLeader and the new leader will pick up the same gossip view.
type MembershipHandler struct {
	svc        raftapi.Service
	membership cluster.Membership
	bus        event.Bus
	logger     *zap.Logger
	tel        *telemetry

	// Reconcile coordination.
	reconcileCh chan struct{}
	stopCh      chan struct{}
	// churnTimes records timestamps of recent voter ops so a sustained burst
	// (>3 in 60s) can be flagged via the raft_voter_churn_burst counter.
	churnTimes []time.Time
	churnMu    sync.Mutex
	wg         sync.WaitGroup
	stopOnce   sync.Once
	cfg        HandlerConfig
}

// NewMembershipHandler builds a handler. `membership` is required: the
// reconciler reads node metadata (raft_eligible, raft_priority, failure_domain)
// from it directly rather than re-deriving from event payloads, so a single
// reconcile pass observes a coherent snapshot.
func NewMembershipHandler(
	svc raftapi.Service,
	membership cluster.Membership,
	bus event.Bus,
	cfg HandlerConfig,
	logger *zap.Logger,
) *MembershipHandler {
	h := &MembershipHandler{
		svc:         svc,
		membership:  membership,
		bus:         bus,
		logger:      logger,
		cfg:         cfg.applyDefaults(logger),
		reconcileCh: make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
	}
	// If the underlying service exposes a telemetry handle, capture it so
	// we can emit voter-op metrics. Non-Node implementations skip silently.
	if tp, ok := svc.(interface{ Telemetry() *telemetry }); ok {
		h.tel = tp.Telemetry()
	}
	return h
}

// Start subscribes to the cluster event stream and launches the reconcile
// loop. Returns once subscription is established.
func (h *MembershipHandler) Start(ctx context.Context) error {
	if h.membership == nil {
		return errors.New("MembershipHandler requires a cluster.Membership instance")
	}

	// Inform the underlying Raft node of the configured voter cap so its
	// voter-ladder telemetry can publish the ceiling alongside live counts.
	// Implemented as a type assertion to keep raftapi.Service a minimal
	// interface; non-Node implementations simply skip the hook.
	if vc, ok := h.svc.(interface{ SetVoterCap(int) }); ok {
		vc.SetVoterCap(h.cfg.MaxVoters)
	}

	ch := make(chan event.Event, busBuffer)
	subID, err := h.bus.Subscribe(ctx, cluster.System, ch)
	if err != nil {
		return fmt.Errorf("subscribe to cluster events: %w", err)
	}

	h.wg.Add(2)
	go h.subscriberLoop(ctx, ch, subID)
	go h.reconcileLoop(ctx)

	// Kick an immediate reconcile so peers already in gossip when we
	// subscribe (i.e. that emitted NodeJoined events before subscription)
	// are picked up. Without this the leader can sit forever as a single
	// voter while followers wait for an AppendEntries that never comes.
	h.scheduleReconcile()
	return nil
}

// Stop terminates the handler. Idempotent.
func (h *MembershipHandler) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopCh)
	})
	h.wg.Wait()
}

// SetMaxVoters updates the voter ceiling at runtime and triggers a reconcile
// so the new cap takes effect without a restart. The new value is clamped
// to a valid odd value (same rule as applyDefaults). Returns the value
// actually applied — useful for callers that want to log discrepancies.
func (h *MembershipHandler) SetMaxVoters(n int) int {
	if n <= 0 {
		n = defaultMaxVoters
	}
	if n%2 == 0 {
		n--
	}
	if n < 1 {
		n = 1
	}
	h.cfg.MaxVoters = n
	if vc, ok := h.svc.(interface{ SetVoterCap(int) }); ok {
		vc.SetVoterCap(n)
	}
	h.logger.Info("max_voters updated at runtime", zap.Int("max_voters", n))
	h.scheduleReconcile()
	return n
}

// subscriberLoop drains the event channel and signals the reconcile loop.
// Decoupling drain from work ensures we never block the global dispatcher,
// even if a reconcile pass takes longer than expected.
func (h *MembershipHandler) subscriberLoop(ctx context.Context, ch <-chan event.Event, subID event.SubscriberID) {
	defer h.wg.Done()
	defer h.bus.Unsubscribe(ctx, subID)

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			switch e.Kind {
			case cluster.NodeJoined, cluster.NodeLeft, cluster.NodeUpdated:
				h.scheduleReconcile()
			case cluster.LeaderElected:
				// Just-won-the-election: reconcile immediately so the new
				// leader applies the cap without waiting for the debounce.
				h.scheduleReconcile()
			}
		case <-h.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// scheduleReconcile is a non-blocking signal: if a reconcile is already
// pending, additional signals coalesce into one.
func (h *MembershipHandler) scheduleReconcile() {
	select {
	case h.reconcileCh <- struct{}{}:
	default:
		// Already scheduled — coalesce.
	}
}

// reconcileLoop debounces signals and runs reconcile passes one at a time.
func (h *MembershipHandler) reconcileLoop(ctx context.Context) {
	defer h.wg.Done()

	var debounceTimer *time.Timer
	for {
		select {
		case <-h.stopCh:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return
		case <-h.reconcileCh:
			// Reset/start debounce window.
			if debounceTimer == nil {
				debounceTimer = time.NewTimer(h.cfg.ReconcileDebounce)
			} else {
				if !debounceTimer.Stop() {
					// Drain if it already fired but we hadn't read it.
					select {
					case <-debounceTimer.C:
					default:
					}
				}
				debounceTimer.Reset(h.cfg.ReconcileDebounce)
			}

		case <-timerC(debounceTimer):
			debounceTimer = nil
			h.runReconcileOnce(ctx)
		}
	}
}

// timerC returns a nil channel when t is nil, so select skips that arm.
func timerC(t *time.Timer) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// runReconcileOnce computes the diff and applies ops. Bounded by
// ReconcileTimeout. Soft errors (ErrNotLeader, ErrLeadershipLost) abort
// the pass without escalating.
func (h *MembershipHandler) runReconcileOnce(ctx context.Context) {
	if !h.svc.IsLeader() {
		return
	}

	passCtx, cancel := context.WithTimeout(ctx, h.cfg.ReconcileTimeout)
	defer cancel()

	nodes := h.membership.Nodes()
	// Include the local node in the candidate set so the reconciler keeps
	// the local raft instance as a voter. Membership.Nodes() omits the local
	// node by design, but reconcileDiff treats absence as "remove from voter
	// set" and the local node would get evicted on every pass.
	if local := h.membership.LocalNode(); local.ID != "" {
		nodes = append(nodes, local)
	}
	candidates := candidatesFromMembership(nodes)

	current, err := h.svc.GetConfiguration()
	if err != nil {
		h.logger.Warn("reconcile: GetConfiguration failed", zap.Error(err))
		return
	}

	currentVoters := make(map[cluster.NodeID]struct{}, len(current))
	for _, s := range current {
		if s.IsVoter {
			currentVoters[s.ID] = struct{}{}
		}
	}

	target := desiredVoterCount(len(candidates), h.cfg.MaxVoters)
	picked := pickVoters(candidates, currentVoters, target)

	// Bound the Raft configuration to voters + a small standby pool. Nodes
	// beyond MaxVoters+MaxStandbys are left out of Raft entirely, so the
	// leader never replicates AppendEntries to them — idle leader cost stays
	// O(1) in cluster size. Non-members reach Raft-backed state by
	// forwarding to a voter.
	members := raftMembers(candidates, picked, h.cfg.MaxStandbys)

	addrLookup := make(map[cluster.NodeID]string, len(candidates))
	for _, c := range candidates {
		addrLookup[c.ID] = c.Addr
	}

	ops := reconcileDiff(picked, members, current, addrLookup)

	// Proactive eviction: any current voter whose peerStateTracker streak
	// exceeds the threshold AND who isn't in the candidate set (gossip has
	// already declared them gone) gets an opRemove appended. Without this
	// branch we'd wait for gossip to fully expire the suspect — slow under
	// chaos. Gossip is still the authority: a voter that gossip lists as
	// alive does NOT get evicted no matter how many heartbeats fail.
	candidateSet := make(map[cluster.NodeID]struct{}, len(candidates))
	for _, c := range candidates {
		candidateSet[c.ID] = struct{}{}
	}
	if streaker, ok := h.svc.(interface {
		PeerDeadStreak(raftapi.ServerAddress) int
	}); ok {
		for _, srv := range current {
			if !srv.IsVoter {
				continue
			}
			if _, gossipKnows := candidateSet[srv.ID]; gossipKnows {
				continue // gossip says alive — defer to it
			}
			if streaker.PeerDeadStreak(srv.Address) <= 0 {
				continue
			}
			// Avoid duplicate ops if reconcileDiff already added a remove.
			already := false
			for _, op := range ops {
				if op.Kind == opRemove && op.ID == srv.ID {
					already = true
					break
				}
			}
			if already {
				continue
			}
			ops = append(ops, membershipOp{Kind: opRemove, ID: srv.ID, Addr: srv.Address})
			if h.tel != nil {
				h.tel.recordProactiveVoterEviction(srv.ID)
			}
			h.logger.Warn("reconcile: proactive voter eviction",
				zap.String("node", srv.ID),
				zap.String("addr", srv.Address),
			)
		}
	}

	if len(ops) == 0 {
		return
	}

	h.logger.Info("raft membership reconcile",
		zap.Int("eligible", len(candidates)),
		zap.Int("target_voters", target),
		zap.Int("ops", len(ops)),
	)

	// Per-op timeout: split the pass budget so a stuck op cannot eat the
	// whole window. Floor at 250ms because Raft round-trips need that much.
	perOp := h.cfg.ReconcileTimeout / time.Duration(len(ops))
	if perOp < 250*time.Millisecond {
		perOp = 250 * time.Millisecond
	}

	for _, op := range ops {
		if passCtx.Err() != nil {
			h.logger.Warn("reconcile: pass context expired before completion",
				zap.Int("remaining_ops", len(ops)))
			return
		}
		if err := h.applyOp(op, perOp); err != nil {
			if isSoftRaftError(err) {
				h.logger.Info("reconcile: soft error, aborting pass",
					zap.String("op", opName(op.Kind)),
					zap.String("node", op.ID),
					zap.Error(err))
				return
			}
			h.logger.Error("reconcile: op failed",
				zap.String("op", opName(op.Kind)),
				zap.String("node", op.ID),
				zap.Error(err))
			// Continue: independent ops should not block each other.
		}
	}
}

// applyOp dispatches a single membership change. Self-removal of the local
// leader transfers leadership first so the cluster does not lose its leader
// during the reconfiguration. Each op increments raft_voter_operations_total
// so the soak surfaces unexpected churn.
func (h *MembershipHandler) applyOp(op membershipOp, timeout time.Duration) error {
	var err error
	switch op.Kind {
	case opAddVoter, opPromote:
		err = h.svc.AddVoter(op.ID, op.Addr, timeout)
	case opAddNonvoter:
		err = h.svc.AddNonvoter(op.ID, op.Addr, timeout)
	case opDemote:
		// Demoting the local leader is unsupported by hashicorp/raft —
		// transfer leadership first.
		if h.isLocalLeader(op.ID) {
			if terr := h.svc.LeadershipTransfer("", timeout); terr != nil {
				err = fmt.Errorf("leadership transfer before self-demote: %w", terr)
			} else {
				err = raftapi.ErrLeadershipLost
			}
		} else {
			err = h.svc.DemoteVoter(op.ID, timeout)
		}
	case opRemove:
		if h.isLocalLeader(op.ID) {
			if terr := h.svc.LeadershipTransfer("", timeout); terr != nil {
				err = fmt.Errorf("leadership transfer before self-remove: %w", terr)
			} else {
				err = raftapi.ErrLeadershipLost
			}
		} else {
			err = h.svc.RemoveServer(op.ID, timeout)
		}
	default:
		return fmt.Errorf("unknown op kind: %d", op.Kind)
	}

	result := "ok"
	if err != nil {
		result = "err"
	}
	if h.tel != nil {
		h.tel.recordVoterOperation(opName(op.Kind), result)
	}
	if result == "ok" {
		h.recordChurnTick()
	}
	return err
}

// recordChurnTick keeps a sliding 60s window of voter-op timestamps and
// emits raft_voter_churn_burst_total when more than 3 ops land in that
// window — a sustained-churn signal worth flagging in dashboards.
func (h *MembershipHandler) recordChurnTick() {
	if h.tel == nil {
		return
	}
	now := time.Now()
	cutoff := now.Add(-60 * time.Second)

	h.churnMu.Lock()
	// Drop expired entries.
	kept := 0
	for _, t := range h.churnTimes {
		if t.After(cutoff) {
			h.churnTimes[kept] = t
			kept++
		}
	}
	h.churnTimes = append(h.churnTimes[:kept], now)
	burst := len(h.churnTimes) > 3
	h.churnMu.Unlock()

	if burst {
		h.tel.recordVoterChurnBurst()
	}
}

// isLocalLeader returns true if `id` is the local node and the local node
// is the current leader. Cheap: one Leader() call.
func (h *MembershipHandler) isLocalLeader(id cluster.NodeID) bool {
	if !h.svc.IsLeader() {
		return false
	}
	return h.membership.LocalNode().ID == id
}

// isSoftRaftError returns true for errors that mean "try again later" rather
// than "this is broken". A soft error aborts the current pass; the next
// gossip event (or the new leader's first reconcile) picks up the work.
func isSoftRaftError(err error) bool {
	return errors.Is(err, raftapi.ErrNotLeader) ||
		errors.Is(err, raftapi.ErrLeadershipLost) ||
		errors.Is(err, raftapi.ErrTimeout)
}

func opName(k opKind) string {
	switch k {
	case opAddVoter:
		return "add-voter"
	case opAddNonvoter:
		return "add-nonvoter"
	case opPromote:
		return "promote"
	case opDemote:
		return "demote"
	case opRemove:
		return "remove"
	default:
		return "unknown"
	}
}

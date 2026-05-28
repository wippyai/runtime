// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"go.uber.org/zap"
)

// Gossip metadata keys used by the bootstrap watcher to discover peers and
// coordinate cluster formation. These keys ride on the existing memberlist
// node metadata (alongside raft_eligible / raft_priority).
const (
	// raftStatusMeta carries the per-node bootstrap state. A node in "pre"
	// is waiting for the watcher to form a cluster; "in" means raft is
	// established (either bootstrapped here or added by the leader). Any
	// node observing peers in "in" knows the cluster has already formed.
	raftStatusMeta = "raft_status"

	raftStatusPre = "pre"
	raftStatusIn  = "in"

	// bootstrapExpectMeta advertises the local BootstrapExpect so peers can
	// discriminate "expected initial member" from "later joiner": only
	// peers advertising the same expect value count toward the quorum.
	bootstrapExpectMeta = "bootstrap_expect"
)

const (
	// Default cadence/window for the watcher loop. Tests override via
	// BootstrapWatcherConfig.
	bootstrapDefaultGrace = 5 * time.Second
	bootstrapDefaultPoll  = 1 * time.Second
	// Event-bus subscription buffer for membership events. Sized for
	// burst rather than throughput; events coalesce in evalQuorum.
	bootstrapBusBufSize = 64
)

// bootstrapMembership is the shape the watcher needs from the membership
// service. Kept narrow so tests can supply a stub.
type bootstrapMembership interface {
	Nodes() []cluster.NodeInfo
	LocalNode() cluster.NodeInfo
	UpdateMeta(updates map[string]string)
}

// bootstrapNode is the slice of *Node the watcher needs. Kept narrow so
// tests can supply a stub.
type bootstrapNode interface {
	Bootstrap(voterIDs []string) error
	State() raftapi.State
	Leader() (raftapi.ServerID, raftapi.ServerAddress, error)
}

// BootstrapWatcherConfig holds the knobs for BootstrapWatcher.
type BootstrapWatcherConfig struct {
	// Expect is the BootstrapExpect value from raft Config: 0 = never
	// self-bootstrap, 1 = bootstrap immediately with self, N>=2 = wait
	// for N alive eligible peers advertising the same expect.
	Expect int
	// Grace is how long the candidate set must remain exactly N before
	// the watcher fires BootstrapCluster. Defaults to 5s if zero.
	Grace time.Duration
	// Poll is the tick cadence for re-evaluating the gossip view.
	// Defaults to 1s if zero. Events drive the fast path; the tick is a
	// safety net so a node that started before peers gossip-converged
	// still progresses.
	Poll time.Duration
}

// BootstrapWatcher implements gossip-driven raft cluster formation
// (Consul/Nomad pattern). The watcher runs on every node; on startup it
// advertises raft_status=pre and observes the gossip view. Once the
// quorum predicate is satisfied — exactly Expect raft-eligible peers
// advertising the same Expect and raft_status=pre, stable for Grace —
// every node deterministically derives the same sorted voter list and
// calls Node.Bootstrap with it.
//
// A node that observes any peer already in raft_status=in skips bootstrap
// (the cluster has formed elsewhere) and waits for the leader's
// reconciler to add it via AddVoter. Once the local raft has a leader
// (State is Leader or Follower with a known leader), the watcher
// transitions itself to raft_status=in and exits.
type BootstrapWatcher struct {
	cfg     BootstrapWatcherConfig
	node    bootstrapNode
	member  bootstrapMembership
	bus     event.Bus
	logger  *zap.Logger
	localID string

	stopCh chan struct{}
	doneCh chan struct{}
	mu     sync.Mutex
	state  string // "pre" or "in"
}

// NewBootstrapWatcher wires the watcher. Start must be called separately
// after the raft Node has Start()'d (so node.Bootstrap is callable) and
// the membership service is up (so Nodes()/UpdateMeta work).
func NewBootstrapWatcher(
	localID string,
	cfg BootstrapWatcherConfig,
	node bootstrapNode,
	member bootstrapMembership,
	bus event.Bus,
	logger *zap.Logger,
) *BootstrapWatcher {
	if cfg.Grace <= 0 {
		cfg.Grace = bootstrapDefaultGrace
	}
	if cfg.Poll <= 0 {
		cfg.Poll = bootstrapDefaultPoll
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &BootstrapWatcher{
		cfg:     cfg,
		node:    node,
		member:  member,
		bus:     bus,
		logger:  logger,
		localID: localID,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
		state:   raftStatusPre,
	}
}

// Start launches the watcher goroutine. Returns immediately; the watcher
// runs until it either successfully bootstraps, observes the cluster
// already formed (and the local raft acquires a leader), or Stop is
// called.
func (w *BootstrapWatcher) Start(ctx context.Context) error {
	// Advertise our presence to peers so they can find us and we count
	// toward their quorum. raft_eligible/raft_priority are set elsewhere;
	// here we add the watcher-coordinated keys.
	w.member.UpdateMeta(map[string]string{
		raftStatusMeta:      raftStatusPre,
		bootstrapExpectMeta: strconv.Itoa(w.cfg.Expect),
	})

	// Expect == 0: never self-bootstrap. Transition to "in" once raft
	// acquires a leader (via the reconciler's AddVoter from the existing
	// cluster). Expect == 1: bootstrap immediately with self.
	if w.cfg.Expect == 1 {
		w.logger.Info("bootstrap watcher: single-node bootstrap")
		if err := w.node.Bootstrap([]string{w.localID}); err != nil {
			close(w.doneCh)
			return err
		}
		w.transitionToIn()
		close(w.doneCh)
		return nil
	}

	ch := make(chan event.Event, bootstrapBusBufSize)
	subID, err := w.bus.Subscribe(ctx, cluster.System, ch)
	if err != nil {
		close(w.doneCh)
		return err
	}

	go w.run(ctx, ch, subID)
	return nil
}

// Stop terminates the watcher. Idempotent. Blocks until the goroutine
// exits.
func (w *BootstrapWatcher) Stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
	<-w.doneCh
}

// run is the main watcher loop. It coalesces gossip events and the
// periodic tick into single re-evaluations of the bootstrap predicate.
func (w *BootstrapWatcher) run(ctx context.Context, ch <-chan event.Event, subID event.SubscriberID) {
	defer close(w.doneCh)
	defer w.bus.Unsubscribe(ctx, subID)

	ticker := time.NewTicker(w.cfg.Poll)
	defer ticker.Stop()

	// stableSince tracks when we first saw the candidate set at exactly N.
	// Resets whenever the set size changes or the set membership shifts.
	var stableSince time.Time
	var lastQuorum []string

	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			switch e.Kind {
			case cluster.NodeJoined, cluster.NodeLeft, cluster.NodeUpdated:
			default:
				continue
			}
		case <-ticker.C:
		}

		// If raft has acquired a leader, transition and exit. This handles
		// the late-joiner path where AddVoter happens before our quorum
		// predicate triggers.
		if w.raftEstablished() {
			w.transitionToIn()
			return
		}

		// Quorum predicate.
		quorum, stable := w.evalQuorum()
		if !stable {
			// Either size != Expect, peer in "in" state observed (cluster
			// formed elsewhere), or set membership changed since last eval.
			stableSince = time.Time{}
			lastQuorum = nil
			continue
		}

		// Set is exactly Expect and all peers in "pre". Track stability.
		if !sameSet(lastQuorum, quorum) {
			lastQuorum = quorum
			stableSince = time.Now()
			continue
		}
		if time.Since(stableSince) < w.cfg.Grace {
			continue
		}

		// Stable for Grace window — fire bootstrap.
		w.logger.Info("bootstrap watcher: quorum reached, forming cluster",
			zap.Strings("voters", quorum),
			zap.Int("expect", w.cfg.Expect))
		if err := w.node.Bootstrap(quorum); err != nil {
			w.logger.Error("bootstrap failed; will retry on next event", zap.Error(err))
			// Reset stability so we don't tight-loop bootstrap.
			stableSince = time.Time{}
			lastQuorum = nil
			continue
		}
		w.transitionToIn()
		return
	}
}

// evalQuorum inspects the current gossip view and returns the
// deterministically-ranked voter IDs and whether the set is bootstrappable.
// Bootstrappable means: no peer reports raft_status=in (the cluster
// hasn't formed elsewhere), and exactly Expect raft-eligible peers (incl.
// self) advertise raft_status=pre with a matching BootstrapExpect.
//
// Ranking shares candidateFromNode + rankCandidates with the membership
// reconciler so the initial-bootstrap voter set agrees with the post-form
// reconciler's preference (priority asc, then ID asc, with raft_eligible
// + failure_domain honored). Without this, the watcher and reconciler can
// pick different voter sets from the same gossip view.
func (w *BootstrapWatcher) evalQuorum() ([]string, bool) {
	nodes := w.member.Nodes()
	local := w.member.LocalNode()

	pool := make([]cluster.NodeInfo, 0, len(nodes)+1)
	pool = append(pool, local)
	for _, n := range nodes {
		if n.ID == local.ID {
			continue // dedupe self if Nodes() includes the local node
		}
		pool = append(pool, n)
	}

	// If any node in our view says it's already in the cluster, defer.
	for _, n := range pool {
		if n.Meta[raftStatusMeta] == raftStatusIn {
			return nil, false
		}
	}

	out := make([]candidate, 0, w.cfg.Expect)
	for _, n := range pool {
		c, ok := candidateFromNode(n)
		if !ok {
			continue
		}
		if n.Meta[raftStatusMeta] != raftStatusPre {
			continue
		}
		exp, err := strconv.Atoi(n.Meta[bootstrapExpectMeta])
		if err != nil || exp != w.cfg.Expect {
			continue
		}
		out = append(out, c)
	}
	if len(out) != w.cfg.Expect {
		return nil, false
	}
	rankCandidates(out)

	ids := make([]string, len(out))
	for i, c := range out {
		ids[i] = c.ID
	}
	return ids, true
}

// raftEstablished returns true if the local raft has acquired a leader,
// either because we bootstrapped or because the cluster's existing leader
// added us via AddVoter.
func (w *BootstrapWatcher) raftEstablished() bool {
	st := w.node.State()
	if st != raftapi.Leader && st != raftapi.Follower {
		return false
	}
	id, _, err := w.node.Leader()
	return err == nil && id != ""
}

// transitionToIn advertises raft_status=in via gossip and flips local state.
// Idempotent.
func (w *BootstrapWatcher) transitionToIn() {
	w.mu.Lock()
	if w.state == raftStatusIn {
		w.mu.Unlock()
		return
	}
	w.state = raftStatusIn
	w.mu.Unlock()
	w.member.UpdateMeta(map[string]string{raftStatusMeta: raftStatusIn})
	w.logger.Info("bootstrap watcher: cluster established, transitioned to 'in'")
}

// sameSet returns true if a and b contain the same elements (assumes both
// are sorted; both are produced by evalQuorum which sorts).
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

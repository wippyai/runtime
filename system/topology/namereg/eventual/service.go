// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

var (
	// ErrServiceStopped is returned when calls land on a stopped service.
	ErrServiceStopped = errors.New("eventualreg: service stopped")
	// ErrNameAlreadyRegistered is returned when a name conflicts with a
	// different PID held locally (the cluster-wide check is best-effort
	// because EVENTUAL is — by design — eventually consistent).
	ErrNameAlreadyRegistered = errors.New("eventualreg: name already registered")
	// ErrNameServiceNotReady is returned by a fresh EVENTUAL register while the
	// node's join-epoch barrier is still in progress. Retryable: the barrier
	// completes shortly after join/rejoin.
	ErrNameServiceNotReady = errors.New("eventualreg: name service not ready: join-epoch barrier in progress")
)

// PeerInventory abstracts the source of "alive peer" node strings. The
// boot component wires this to the membership service.
type PeerInventory interface {
	// AlivePeers returns the node strings of all currently-alive peers,
	// excluding the local node.
	AlivePeers() []string
}

// CrossScopeChecker abstracts the CONSISTENT/LOCAL registries so EVENTUAL
// registrations can refuse to shadow them. Returning a non-empty PID with
// `found=true` means the name is held in another scope.
type CrossScopeChecker interface {
	// LookupOther returns (PID, true) if the name is held in any non-Eventual
	// scope (Consistent via Raft, or Local via PIDRegistry).
	LookupOther(name string) (pid.PID, bool)
	// NameReady reports whether the node's join-epoch barrier has completed. A
	// fresh EVENTUAL register is refused (ErrNameServiceNotReady) until it is true
	// so the node cannot shadow a cluster-wide Strong name it has not yet learned.
	NameReady() bool
}

// MessageSender ships a targeted reliable frame to a specific peer.
// Used by the shard-pull anti-entropy path: when MergeRemoteState
// detects a digest mismatch, the service emits a FrameTypeShardRequest
// to the divergent peer; the peer's NotifyMsg path handles the
// response. Decoupled from membership so the package stays
// testable without spinning a memberlist.
type MessageSender interface {
	// Send delivers `payload` to `targetNode` reliably (TCP). The
	// payload already carries the eventualreg type byte; the sender
	// only wraps with the membership multiplex header.
	Send(targetNode string, payload []byte) error
}

// Config configures a Service.
type Config struct {
	// Peers supplies the current alive peer set.
	Peers PeerInventory
	// CrossScope optionally cross-checks CONSISTENT/LOCAL on Register.
	CrossScope CrossScopeChecker
	// MetricsCollector may be nil.
	MetricsCollector metrics.Collector
	// Logger may be nil.
	Logger *zap.Logger
	// Bus delivers cluster.NodeLeft events so the service can reap a
	// departed node's live bindings. When nil, node-leave reaping is
	// disabled (tombstones still age out via wall-floor GC).
	Bus event.Bus
	// Sender ships targeted reliable frames for the shard-pull path.
	// When nil, mismatch detection still works but the recovery
	// channel is offline (convergence falls back to GC).
	Sender MessageSender
	// Revoker delivers name_revoked notifications to local processes that
	// lost a name to a different origin. It is the relay router: a package
	// targeted at the local PID on TopicEvents lands in that process's
	// mailbox. When nil, loss detection still mutates state correctly but no
	// signal is delivered.
	Revoker relay.Receiver
	// LocalNodeID is the string nodeID of this replica.
	LocalNodeID string
	// AntiEntropyPeriod is the cadence at which we expect Delegate.LocalState
	// to be invoked by the transport. Used for telemetry (convergence lag).
	// Default 10 s.
	AntiEntropyPeriod time.Duration
	// GCPeriod is the tombstone reap cadence. Default 20 s.
	GCPeriod time.Duration
	// WallFloor caps tombstone age when positive. Default 0 disables
	// time-based tombstone expiry; safe-counter GC remains enabled.
	WallFloor time.Duration
	// ShardRequestCooldown suppresses repeated shard requests to the
	// same peer within this window — keeps a sustained mismatch from
	// generating a request per push/pull tick. Default 5 s.
	ShardRequestCooldown time.Duration
	// BroadcastCap is the per-node cap on pending deltas before drop. Default 4096.
	BroadcastCap int
}

// Service is the gossip-based name registry.
type Service struct {
	state   *State
	queue   *BroadcastQueue
	tracker *TombstoneTracker
	gc      *GCRunner
	tel     *telemetry
	logger  *zap.Logger
	// lastShardRequest tracks per-peer cooldown for shard-pull requests.
	// Key: peer node string. Value: unix-nanos of the last request emitted.
	// Reads/writes are guarded by lastShardRequestMu.
	lastShardRequest map[string]int64
	nodeLeftSub      *eventbus.Subscriber
	cfg              Config
	stopOnce         sync.Once
	// lastShardRequestMu guards lastShardRequest.
	lastShardRequestMu sync.Mutex
	stopped            atomic.Bool
}

// NewService constructs a Service. Must call Start before use.
func NewService(cfg Config) *Service {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.AntiEntropyPeriod <= 0 {
		cfg.AntiEntropyPeriod = 10 * time.Second
	}
	if cfg.GCPeriod <= 0 {
		cfg.GCPeriod = 20 * time.Second
	}
	if cfg.BroadcastCap <= 0 {
		cfg.BroadcastCap = 4096
	}
	if cfg.ShardRequestCooldown <= 0 {
		cfg.ShardRequestCooldown = 5 * time.Second
	}

	state := NewState(cfg.LocalNodeID)
	queue := NewBroadcastQueue(cfg.LocalNodeID, cfg.BroadcastCap, state.NodeString)
	tracker := NewTombstoneTracker()
	tel := newTelemetry(cfg.MetricsCollector, cfg.LocalNodeID)

	s := &Service{
		cfg:              cfg,
		state:            state,
		queue:            queue,
		tracker:          tracker,
		tel:              tel,
		logger:           cfg.Logger.Named("eventualreg"),
		lastShardRequest: map[string]int64{},
	}

	gcCfg := GCConfig{
		State:     state,
		Tracker:   tracker,
		AliveFn:   func() map[string]struct{} { return s.aliveSet() },
		Period:    cfg.GCPeriod,
		WallFloor: cfg.WallFloor,
		OnSafe: func(n int) {
			tel.recordTombstoneGC("safe_counter", n)
		},
		OnWallFloor: func(n int) {
			tel.recordTombstoneGC("wall_floor", n)
		},
		OnReclaim: func(n int) {
			tel.recordNodeReclaim(n)
		},
	}
	s.gc = NewGCRunner(gcCfg)

	return s
}

// Start begins background tasks (GC reaper) and subscribes to cluster
// NodeLeft so a departed node's live bindings get reaped.
func (s *Service) Start(ctx context.Context) error {
	s.gc.Start()
	if s.cfg.Bus != nil {
		sub, err := eventbus.NewSubscriber(ctx, s.cfg.Bus, cluster.System, cluster.NodeLeft, s.onNodeLeftEvent)
		if err != nil {
			return err
		}
		s.nodeLeftSub = sub
	}
	s.logger.Info("eventualreg started",
		zap.String("node", s.cfg.LocalNodeID),
		zap.Duration("anti_entropy_period", s.cfg.AntiEntropyPeriod),
		zap.Duration("gc_period", s.cfg.GCPeriod))
	return nil
}

// Stop halts background tasks.
func (s *Service) Stop() error {
	s.stopOnce.Do(func() {
		s.stopped.Store(true)
		if s.nodeLeftSub != nil {
			s.nodeLeftSub.Close()
		}
		s.gc.Stop()
	})
	s.logger.Info("eventualreg stopped")
	return nil
}

// --- Public API ---

// RegisterOption configures a Register call.
type RegisterOption func(*registerOptions)

type registerOptions struct {
	priority uint32
}

// WithPriority sets the cross-origin conflict precedence for the registration.
// Higher priority wins concurrent cross-origin conflicts regardless of arrival
// order. Default 0.
func WithPriority(p uint32) RegisterOption {
	return func(o *registerOptions) { o.priority = p }
}

// Register associates `name` with `p` in the eventual cluster registry.
// Returns the registered PID and nil on success. Returns the existing PID and
// ErrNameAlreadyRegistered when the name is held locally by a different PID, or
// when a different-origin entry out-ranks this fresh claim (the caller lost the
// concurrent conflict — a name_revoked is also signaled to `p`). Cross-scope
// conflicts (CONSISTENT/LOCAL) are rejected.
func (s *Service) Register(name string, p pid.PID) (pid.PID, error) {
	return s.register(name, p)
}

// RegisterWithOptions is Register with conflict-resolution options.
func (s *Service) RegisterWithOptions(name string, p pid.PID, opts ...RegisterOption) (pid.PID, error) {
	return s.register(name, p, opts...)
}

func (s *Service) register(name string, p pid.PID, opts ...RegisterOption) (pid.PID, error) {
	if s.stopped.Load() {
		return pid.PID{}, ErrServiceStopped
	}

	var o registerOptions
	for _, opt := range opts {
		opt(&o)
	}

	// Cross-scope check first — refuse to shadow CONSISTENT or LOCAL.
	if s.cfg.CrossScope != nil {
		if existing, found := s.cfg.CrossScope.LookupOther(name); found {
			if existing == p {
				return p, nil
			}
			s.tel.recordRegister("conflict_other_scope")
			return existing, ErrNameAlreadyRegistered
		}
		// Join-epoch gate: refuse a fresh claim while the barrier is in progress,
		// unless this node already holds the name to the same pid (re-register is
		// safe — no shadowing risk).
		if !s.cfg.CrossScope.NameReady() {
			if cur, ok := s.state.Lookup(name); ok && cur == p {
				return p, nil
			}
			s.tel.recordRegister("not_ready")
			return p, ErrNameServiceNotReady
		}
	}

	res := s.state.Register(name, p, time.Now().UnixMilli(), o.priority)
	if !res.Won {
		if res.Lost != nil {
			// Cross-origin loss: the local dot was minted and installed, so
			// broadcast it for cluster convergence, then signal the loser.
			s.queue.Push(res.Entry)
			s.emitRevoke(res.Lost)
			s.tel.recordRegister("conflict_cross_origin")
			s.tel.setEntries(s.state.LiveCount(), s.state.TombstoneCount())
			s.tel.setQueueDepth(s.queue.Depth())
		} else {
			// Same-node, different PID: hard local rejection, no dot minted.
			s.tel.recordRegister("conflict_local")
		}
		return res.Winner.PID, ErrNameAlreadyRegistered
	}
	s.queue.Push(res.Entry)
	s.tel.recordRegister("ok")
	s.tel.setEntries(s.state.LiveCount(), s.state.TombstoneCount())
	s.tel.setQueueDepth(s.queue.Depth())
	return p, nil
}

// emitRevoke delivers a name_revoked notification to the local process named by
// the lost binding. It fires once per transition naturally: State.Apply reports
// a LostBinding only when a merge changes the winner away from a local live dot,
// and re-applying the same remote winner is a MergeNoop with no LostBinding, so
// anti-entropy replays never re-signal. The Register immediate-loss path is also
// single-shot. The signal carries the exact {Name, PID} so a consumer can ignore
// a stale revoke whose PID no longer matches its current registration.
func (s *Service) emitRevoke(lost *LostBinding) {
	if lost == nil {
		return
	}
	s.tel.recordRevoke()
	if s.cfg.Revoker == nil {
		return
	}
	pkg := topology.CancelPackage(topology.SystemPID, lost.PID, "name revoked: "+lost.Name)
	if err := s.cfg.Revoker.Send(pkg); err != nil {
		s.logger.Debug("eventualreg: deliver name-revoked cancel failed",
			zap.String("name", lost.Name), zap.String("pid", lost.PID.String()), zap.Error(err))
	}
}

// RevokeForStrong tombstones a locally-held EVENTUAL binding of name whose pid
// differs from keep, signaling the losing process. The join-epoch barrier calls
// it after learning name belongs to a Strong reservation owned by keep. Returns
// true when a binding was revoked. A name not held locally, or held to keep, is
// a no-op. The tombstone broadcasts so the cluster converges away from the
// loser.
func (s *Service) RevokeForStrong(name string, keep pid.PID) bool {
	if s.stopped.Load() {
		return false
	}
	cur, ok := s.state.Lookup(name)
	if !ok || cur == keep {
		return false
	}
	e := s.state.Unregister(name, time.Now().UnixMilli())
	if e == nil {
		return false
	}
	s.queue.Push(e)
	s.emitRevoke(&LostBinding{Name: name, PID: cur})
	s.tel.setEntries(s.state.LiveCount(), s.state.TombstoneCount())
	s.tel.setQueueDepth(s.queue.Depth())
	return true
}

// Unregister tombstones a name. Returns true if the name was held by us.
func (s *Service) Unregister(name string) bool {
	if s.stopped.Load() {
		return false
	}
	e := s.state.Unregister(name, time.Now().UnixMilli())
	if e == nil {
		s.tel.recordUnregister("not_found")
		return false
	}
	s.queue.Push(e)
	s.tel.recordUnregister("ok")
	s.tel.setEntries(s.state.LiveCount(), s.state.TombstoneCount())
	s.tel.setQueueDepth(s.queue.Depth())
	return true
}

// Lookup returns the live PID for a name (Found=true), or a zero LookupResult
// if absent or tombstoned. EventualRegistry has no reverse-by-PID index, so
// ByPID(p) is unsupported — it returns Found=false with an empty NamesForPID
// slice.
func (s *Service) Lookup(_ context.Context, name string, opts ...global.LookupOption) (global.LookupResult, error) {
	var o global.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}

	if o.ByPID != nil {
		return global.LookupResult{PID: *o.ByPID}, nil
	}

	p, found := s.state.Lookup(name)
	return global.LookupResult{
		PID:   p,
		Found: found,
	}, nil
}

// --- Transport hooks (called by delegate.go) ---

// DrainBroadcasts returns batched delta frames ready to send via UDP user
// broadcast. `headerOverhead` is the per-frame budget reserved by memberlist;
// `byteBudget` is the total cost (len(frame)+headerOverhead summed) the caller
// can transmit this round.
func (s *Service) DrainBroadcasts(headerOverhead, byteBudget int) [][]byte {
	frames := s.queue.Drain(headerOverhead, byteBudget)
	for _, f := range frames {
		s.tel.recordDeltaBytes("tx", "delta", len(f))
	}
	s.tel.setQueueDepth(s.queue.Depth())
	s.tel.setQueueDropped(s.queue.Dropped())
	return frames
}

// OnFrame is called by the delegate when an eventualreg frame arrives
// (UDP user-broadcast for delta frames, reliable TCP user-message for
// shard request/response). The first byte selects the parser:
//
//	FrameTypeDelta         → existing CRDT merge path
//	FrameTypeShardRequest  → encode + reply with the requested shards
//	FrameTypeShardResponse → merge each shard payload into local state
//
// Unknown frame types are dropped with a warn log.
func (s *Service) OnFrame(data []byte) {
	if s.stopped.Load() {
		return
	}
	if len(data) == 0 {
		return
	}
	ft := FrameType(data[0])
	body := data[1:]
	switch ft {
	case FrameTypeDelta:
		s.handleDeltaFrame(body)
	case FrameTypeShardRequest:
		s.handleShardRequestFrame(body)
	case FrameTypeShardResponse:
		s.handleShardResponseFrame(body)
	default:
		s.logger.Warn("eventualreg: unknown frame type", zap.Uint8("type", uint8(ft)))
	}
}

func (s *Service) handleDeltaFrame(body []byte) {
	s.tel.recordDeltaBytes("rx", "delta", len(body))
	entries, origins, err := DecodeFrame(body)
	if err != nil {
		s.logger.Warn("eventualreg: malformed delta frame", zap.Error(err))
		// Apply whatever we managed to decode.
	}
	for i := range entries {
		s.applyIncoming(&entries[i], origins[i])
	}
	s.tel.setEntries(s.state.LiveCount(), s.state.TombstoneCount())
}

func (s *Service) handleShardRequestFrame(body []byte) {
	sender, ids, err := DecodeShardRequestFrame(body)
	if err != nil {
		s.logger.Warn("eventualreg: malformed shard request", zap.Error(err))
		return
	}
	if s.cfg.Sender == nil {
		// No reply path; the requester will eventually retry on the next
		// mismatch detection. Telemetry stays silent — this is a
		// receive-side observation, not a request we emitted.
		return
	}
	payloads := make([][]byte, 0, len(ids))
	payloadsSkipped := 0
	maxShardPayloadBytes := ReliableFrameMaxBytes - (2 + len(s.cfg.LocalNodeID))
	for _, id := range ids {
		entries := s.state.ShardEntries(int(id))
		chunks, skipped, err := EncodeShardPayloadsBounded(id, entries, s.state.NodeString, maxShardPayloadBytes)
		if err != nil {
			s.logger.Warn("eventualreg: encode shard", zap.Uint16("shard", id), zap.Error(err))
			continue
		}
		payloadsSkipped += skipped
		payloads = append(payloads, chunks...)
	}
	if len(payloads) == 0 {
		if payloadsSkipped > 0 {
			s.logger.Warn("eventualreg: shard response entries exceeded reliable frame cap",
				zap.String("to", sender),
				zap.Int("skipped", payloadsSkipped),
				zap.Int("max_bytes", ReliableFrameMaxBytes))
		}
		return
	}
	frames, sent, skipped, err := EncodeShardResponseFramesBounded(s.cfg.LocalNodeID, payloads, ReliableFrameMaxBytes)
	if err != nil {
		s.logger.Warn("eventualreg: encode response frame", zap.Error(err))
		return
	}
	if skipped+payloadsSkipped > 0 {
		s.logger.Warn("eventualreg: shard response payloads exceeded reliable frame cap",
			zap.String("to", sender),
			zap.Int("skipped_payloads", skipped),
			zap.Int("skipped_entries", payloadsSkipped),
			zap.Int("max_bytes", ReliableFrameMaxBytes))
	}
	if len(frames) == 0 {
		return
	}
	for _, frame := range frames {
		if err := s.cfg.Sender.Send(sender, frame); err != nil {
			s.logger.Debug("eventualreg: send shard response failed",
				zap.String("to", sender), zap.Error(err))
			return
		}
	}
	s.tel.recordShardResponse("tx", sent)
}

func (s *Service) handleShardResponseFrame(body []byte) {
	sender, rest, err := DecodeShardResponseFrame(body)
	if err != nil {
		s.logger.Warn("eventualreg: malformed shard response", zap.Error(err))
		return
	}
	now := time.Now()
	count := 0
	for len(rest) > 0 {
		payload, consumed, err := DecodeShardPayload(rest)
		if err != nil {
			s.logger.Warn("eventualreg: decode shard payload",
				zap.String("from", sender), zap.Error(err))
			break
		}
		for i := range payload.Entries {
			s.applyIncoming(&payload.Entries[i], payload.Origins[i])
		}
		count++
		rest = rest[consumed:]
	}
	if count > 0 {
		s.tel.recordShardResponse("rx", count)
		s.tel.recordAntiEntropy("ok", float64(time.Since(now).Milliseconds()), count)
		s.tel.setEntries(s.state.LiveCount(), s.state.TombstoneCount())
		// Treat a bulk shard recovery as a cross-subsystem
		// re-registration burst, same shape as MergeShardPayload.
		s.tel.recordReregistration()
	}
}

// RequestShards emits a FrameTypeShardRequest to `peer` listing the
// shard indices the local node currently disagrees with. The cooldown
// suppresses repeated requests to the same peer inside
// `cfg.ShardRequestCooldown`. Returns true when the request was
// emitted (also true when the sender returned an error — telemetry
// captures the failure). Returns false when:
//
//   - a request to this peer fired recently (cooldown not elapsed)
//   - no sender is configured
//   - shardIDs is empty
//
// Designed so the delegate's MergeRemoteState can call it
// unconditionally on a digest mismatch without re-implementing rate
// limiting.
func (s *Service) RequestShards(peer string, shardIDs []uint16) bool {
	if s.stopped.Load() || s.cfg.Sender == nil || peer == "" || len(shardIDs) == 0 {
		return false
	}
	nowNs := time.Now().UnixNano()
	s.lastShardRequestMu.Lock()
	last := s.lastShardRequest[peer]
	cooldown := s.cfg.ShardRequestCooldown.Nanoseconds()
	if last != 0 && nowNs-last < cooldown {
		s.lastShardRequestMu.Unlock()
		s.tel.recordShardRequest("suppressed")
		return false
	}
	s.lastShardRequest[peer] = nowNs
	s.lastShardRequestMu.Unlock()

	frame, err := EncodeShardRequestFrame(s.cfg.LocalNodeID, shardIDs)
	if err != nil {
		s.logger.Warn("eventualreg: encode shard request", zap.Error(err))
		return false
	}
	if err := s.cfg.Sender.Send(peer, frame); err != nil {
		s.tel.recordShardRequest("send_error")
		s.logger.Debug("eventualreg: send shard request failed",
			zap.String("to", peer), zap.Error(err))
		return true
	}
	s.tel.recordShardRequest("sent")
	return true
}

// LocalDigest builds the digest for outgoing push/pull.
func (s *Service) LocalDigest() Digest {
	return MakeDigest(s.state)
}

// LocalShardPayload returns the bulk-transfer payload for a shard.
func (s *Service) LocalShardPayload(shardID uint16) ([]byte, error) {
	if int(shardID) >= ShardCount {
		return nil, errors.New("eventualreg: shard out of range")
	}
	entries := s.state.ShardEntries(int(shardID))
	buf, err := EncodeShardPayload(nil, shardID, entries, s.state.NodeString)
	if err != nil {
		return nil, err
	}
	s.tel.recordDeltaBytes("tx", "full_shard", len(buf))
	return buf, nil
}

// MergeShardPayload applies a peer's shard data and updates trackers.
// `start` is the wall time when the round began (for convergence-lag
// telemetry). `peer` is recorded for diagnostics only.
func (s *Service) MergeShardPayload(peer string, data []byte, start time.Time) error {
	s.tel.recordDeltaBytes("rx", "full_shard", len(data))
	payload, _, err := DecodeShardPayload(data)
	if err != nil {
		s.tel.recordAntiEntropy("decode_error", float64(time.Since(start).Milliseconds()), 0)
		return err
	}
	for i := range payload.Entries {
		s.applyIncoming(&payload.Entries[i], payload.Origins[i])
	}
	// Bulk shard receipt during rejoin counts as cluster-wide re-registrations
	// flowing back into this node — surfaces in the soak gate so a flood
	// would be caught.
	s.tel.recordReregistration()
	s.tel.recordAntiEntropy("ok", float64(time.Since(start).Milliseconds()), 1)
	s.tel.setEntries(s.state.LiveCount(), s.state.TombstoneCount())
	_ = peer
	return nil
}

// OnPeerDigest records that `peer` has acknowledged its own state up to its
// CV; we use this for tombstone GC. The digest itself doesn't carry a CV
// (only hashes), so callers must provide it separately. For memberlist
// push/pull, the CV travels alongside in the LocalState multiplex.
func (s *Service) OnPeerDigest(peer string, peerCV []uint64) {
	s.tracker.RecordAck(peer, peerCV)
}

// OnPeerLeft drops a peer from the tombstone tracker so it stops blocking GC.
func (s *Service) OnPeerLeft(peer string) {
	s.tracker.ForgetPeer(peer)
}

// onNodeLeftEvent handles cluster.NodeLeft: it reaps every live binding whose
// resolved PID lives on the departed node and drops the peer from the
// tombstone tracker. Every surviving node runs this deterministically on the
// same NodeLeft, so the reap converges via gossip.
func (s *Service) onNodeLeftEvent(e event.Event) {
	if s.stopped.Load() {
		return
	}
	ne, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}
	node := ne.Node.ID
	if node == "" || node == s.cfg.LocalNodeID {
		return
	}
	s.handleNodeLeft(node)
}

// handleNodeLeft tombstones every live binding whose resolved PID lives on the
// departed node, forgets the peer in the GC tracker, and drops its shard-pull
// cooldown. The departed node is the origin of its own names, so the reap must
// tombstone the foreign-origin dot in place (State.ReapNode) — Unregister only
// touches the local origin's dot and would be a no-op for a departed peer's
// bindings.
func (s *Service) handleNodeLeft(node string) {
	tombstones := s.state.ReapNode(node)
	for _, e := range tombstones {
		s.queue.Push(e)
	}
	if len(tombstones) > 0 {
		s.tel.setEntries(s.state.LiveCount(), s.state.TombstoneCount())
		s.tel.setQueueDepth(s.queue.Depth())
	}

	s.lastShardRequestMu.Lock()
	delete(s.lastShardRequest, node)
	s.lastShardRequestMu.Unlock()

	s.OnPeerLeft(node)
	if len(tombstones) > 0 {
		s.logger.Info("eventualreg: reaped departed node bindings",
			zap.String("node", node), zap.Int("count", len(tombstones)))
	}
}

// State returns the underlying State for tests and tooling. Not for hot paths.
func (s *Service) State() *State { return s.state }

// Tracker returns the tombstone tracker for tests and tooling.
func (s *Service) Tracker() *TombstoneTracker { return s.tracker }

// CVSnapshot is a convenience for callers that need to attach our CV to push/pull.
func (s *Service) CVSnapshot() []uint64 { return s.state.CVSnapshot() }

// --- topology.EventualRegistry adapter ---

// Ensure Service satisfies topology.EventualRegistry.
var _ topology.EventualRegistry = (*Service)(nil)

// --- internal ---

func (s *Service) applyIncoming(e *Entry, originStr string) {
	// Intern origin if we haven't seen it before so cv tracks it.
	internedOrigin := s.state.internNode(originStr)
	e.Node = internedOrigin

	outcome, _, lost := s.state.Apply(e)

	// Epidemic forwarding: a frame that changed local state is new information,
	// so re-broadcast it. The origin emits each delta one-shot to only
	// GossipNodes peers; without forwarding the rest of the cluster converges
	// solely via slow anti-entropy. Loop-free because a re-applied entry is a
	// MergeNoop and is not re-queued. The queued entry is a copy: State retains
	// `e` and may mutate it on later merges.
	if outcome == MergeApplied || outcome == MergeConflictResolved || outcome == MergeDeleteWins {
		cp := *e
		s.queue.Push(&cp)
		s.tel.setQueueDepth(s.queue.Depth())
	}

	switch outcome {
	case MergeApplied:
		// nothing extra
	case MergeConflictResolved:
		s.tel.recordMergeConflict("concurrent")
		// A local-origin live binding lost to a different origin: signal the
		// home process. Deduped per lost dot, so anti-entropy re-applying the
		// same winner does not re-fire.
		if lost != nil {
			s.emitRevoke(lost)
		}
	case MergeDeleteWins:
		s.tel.recordMergeConflict("delete_wins")
	case MergeNoop:
		if e.Deleted {
			// Late-arriving tombstone for an entry we no longer have.
			s.tel.recordTombstoneLate()
		}
	}
}

// aliveSet returns the current alive peer set (including self) as a lookup
// map. Used by GC to compute safe counters.
func (s *Service) aliveSet() map[string]struct{} {
	out := map[string]struct{}{}
	if s.cfg.Peers != nil {
		for _, p := range s.cfg.Peers.AlivePeers() {
			out[p] = struct{}{}
		}
	}
	out[s.cfg.LocalNodeID] = struct{}{}
	return out
}

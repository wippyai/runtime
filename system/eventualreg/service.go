// SPDX-License-Identifier: MPL-2.0

package eventualreg

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

var (
	// ErrServiceStopped is returned when calls land on a stopped service.
	ErrServiceStopped = errors.New("eventualreg: service stopped")
	// ErrNameAlreadyRegistered is returned when a name conflicts with a
	// different PID held locally (the cluster-wide check is best-effort
	// because EVENTUAL is — by design — eventually consistent).
	ErrNameAlreadyRegistered = errors.New("eventualreg: name already registered")
)

// PeerInventory abstracts the source of "alive peer" node strings. The
// boot component wires this to the membership service.
type PeerInventory interface {
	// AlivePeers returns the node strings of all currently-alive peers,
	// excluding the local node.
	AlivePeers() []string
}

// CrossScopeChecker abstracts the GLOBAL/LOCAL registries so EVENTUAL
// registrations can refuse to shadow them. Returning a non-empty PID with
// `found=true` means the name is held in another scope.
type CrossScopeChecker interface {
	// LookupOther returns (PID, true) if the name is held in any non-Eventual
	// scope (Global via Raft, or Local via PIDRegistry).
	LookupOther(name string) (pid.PID, bool)
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
	// CrossScope optionally cross-checks GLOBAL/LOCAL on Register.
	CrossScope CrossScopeChecker
	// MetricsCollector may be nil.
	MetricsCollector metrics.Collector
	// Logger may be nil.
	Logger *zap.Logger
	// Sender ships targeted reliable frames for the shard-pull path.
	// When nil, mismatch detection still works but the recovery
	// channel is offline (convergence falls back to GC).
	Sender MessageSender
	// LocalNodeID is the string nodeID of this replica.
	LocalNodeID string
	// AntiEntropyPeriod is the cadence at which we expect Delegate.LocalState
	// to be invoked by the transport. Used for telemetry (convergence lag).
	// Default 10 s.
	AntiEntropyPeriod time.Duration
	// GCPeriod is the tombstone reap cadence. Default 20 s.
	GCPeriod time.Duration
	// WallFloor caps tombstone age. Default 15 min.
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
	lastShardRequest   map[string]int64
	cfg                Config
	stopOnce           sync.Once
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
	if cfg.WallFloor <= 0 {
		cfg.WallFloor = DefaultWallFloor
	}
	if cfg.BroadcastCap <= 0 {
		cfg.BroadcastCap = 4096
	}
	if cfg.ShardRequestCooldown <= 0 {
		cfg.ShardRequestCooldown = 5 * time.Second
	}

	state := NewState(cfg.LocalNodeID)
	queue := NewBroadcastQueue(cfg.LocalNodeID, cfg.BroadcastCap)
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
	}
	s.gc = NewGCRunner(gcCfg)

	return s
}

// Start begins background tasks (GC reaper).
func (s *Service) Start(_ context.Context) error {
	s.gc.Start()
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
		s.gc.Stop()
	})
	s.logger.Info("eventualreg stopped")
	return nil
}

// --- Public API ---

// Register associates `name` with `p` in the eventual cluster registry.
// Returns the registered PID and nil on success. Returns the existing PID
// and ErrNameAlreadyRegistered if the name is locally held by a different
// PID. Cross-scope conflicts (GLOBAL/LOCAL) are also rejected.
func (s *Service) Register(name string, p pid.PID) (pid.PID, error) {
	if s.stopped.Load() {
		return pid.PID{}, ErrServiceStopped
	}

	// Cross-scope check first — refuse to shadow GLOBAL or LOCAL.
	if s.cfg.CrossScope != nil {
		if existing, found := s.cfg.CrossScope.LookupOther(name); found {
			if existing == p {
				return p, nil
			}
			s.tel.recordRegister("conflict_other_scope")
			return existing, ErrNameAlreadyRegistered
		}
	}

	e, ok := s.state.Register(name, p, time.Now().UnixMilli())
	if !ok {
		s.tel.recordRegister("conflict_local")
		return e.PID, ErrNameAlreadyRegistered
	}
	s.queue.Push(e)
	s.tel.recordRegister("ok")
	s.tel.setEntries(s.state.LiveCount(), s.state.TombstoneCount())
	s.tel.setQueueDepth(s.queue.Depth())
	return p, nil
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
// if absent or tombstoned. EventualRegistry has no fencing tokens or
// reverse-by-PID index, so WithFence() yields FenceToken=0 and ByPID(p)
// is unsupported — it returns Found=false with an empty NamesForPID slice.
func (s *Service) Lookup(_ context.Context, name string, opts ...globalreg.LookupOption) (globalreg.LookupResult, error) {
	var o globalreg.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}

	if o.ByPID != nil {
		return globalreg.LookupResult{PID: *o.ByPID}, nil
	}

	p, found := s.state.Lookup(name)
	return globalreg.LookupResult{
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
	for _, id := range ids {
		p, err := s.LocalShardPayload(id)
		if err != nil {
			s.logger.Warn("eventualreg: encode shard", zap.Uint16("shard", id), zap.Error(err))
			continue
		}
		payloads = append(payloads, p)
	}
	if len(payloads) == 0 {
		return
	}
	frame, err := EncodeShardResponseFrame(s.cfg.LocalNodeID, payloads)
	if err != nil {
		s.logger.Warn("eventualreg: encode response frame", zap.Error(err))
		return
	}
	if err := s.cfg.Sender.Send(sender, frame); err != nil {
		s.logger.Debug("eventualreg: send shard response failed",
			zap.String("to", sender), zap.Error(err))
		return
	}
	s.tel.recordShardResponse("tx", len(payloads))
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

// State returns the underlying State for tests and tooling. Not for hot paths.
func (s *Service) State() *State { return s.state }

// CVSnapshot is a convenience for callers that need to attach our CV to push/pull.
func (s *Service) CVSnapshot() []uint64 { return s.state.CVSnapshot() }

// CompactNodeIDs returns the string nodeIDs in compact-ID order, so callers
// can serialize the CV alongside it.
func (s *Service) CompactNodeIDs() []string {
	out := make([]string, 0, ShardCount)
	for i := uint32(0); ; i++ {
		name := s.state.NodeString(i)
		if name == "" && i > 0 {
			break
		}
		if i == 0 && name == "" {
			break
		}
		out = append(out, name)
	}
	return out
}

// --- topology.EventualRegistry adapter ---

// Ensure Service satisfies topology.EventualRegistry.
var _ topology.EventualRegistry = (*Service)(nil)

// --- internal ---

func (s *Service) applyIncoming(e *Entry, originStr string) {
	// Intern origin if we haven't seen it before so cv tracks it.
	internedOrigin := s.state.internNode(originStr)
	e.Node = internedOrigin

	outcome, _ := s.state.Apply(e)
	switch outcome {
	case MergeApplied:
		// nothing extra
	case MergeWallTiebreak:
		s.tel.recordMergeConflict("wall_clock")
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

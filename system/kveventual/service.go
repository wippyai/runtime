// SPDX-License-Identifier: MPL-2.0

package kveventual

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/kv"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/system/crdt"
	"go.uber.org/zap"
)

// DelegateKind is the multiplex byte the membership service uses to route
// kveventual frames. Distinct from eventualreg's 0xE1.
const DelegateKind byte = 0xE2

// PeerInventory abstracts the alive-peer source. Boot wires this to the
// runtime membership service.
type PeerInventory interface {
	AlivePeers() []string
}

// Config is the input to NewService.
type Config struct {
	Peers            PeerInventory
	MetricsCollector metrics.Collector
	Logger           *zap.Logger
	LocalNodeID      string
	GCPeriod         time.Duration // default 20s
	WallFloor        time.Duration // default 15min
}

// Service holds all kveventual spaces for a node + provides the Delegate
// for memberlist multiplexing. Spaces are created lazily via Open.
type Service struct {
	logger   *zap.Logger
	tel      *telemetry
	stopCh   chan struct{}
	spaces   sync.Map // name → *space
	cfg      Config
	stopOnce sync.Once
	stopped  atomic.Bool
}

// NewService constructs the service. Caller must Start before use.
func NewService(cfg Config) *Service {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.GCPeriod <= 0 {
		cfg.GCPeriod = 20 * time.Second
	}
	if cfg.WallFloor <= 0 {
		cfg.WallFloor = 15 * time.Minute
	}
	return &Service{
		cfg:    cfg,
		logger: cfg.Logger.Named("kveventual"),
		tel:    newTelemetry(cfg.MetricsCollector, cfg.LocalNodeID),
		stopCh: make(chan struct{}),
	}
}

// Start kicks off the GC loop.
func (s *Service) Start(_ context.Context) error {
	go s.gcLoop()
	s.logger.Info("kveventual started",
		zap.String("node", s.cfg.LocalNodeID),
		zap.Duration("gc_period", s.cfg.GCPeriod))
	return nil
}

// Stop releases all resources. Subsequent ops on returned KV handles
// receive ErrSpaceClosed.
func (s *Service) Stop() error {
	s.stopOnce.Do(func() {
		s.stopped.Store(true)
		close(s.stopCh)
		s.spaces.Range(func(_, v any) bool {
			if sp, ok := v.(*space); ok {
				_ = sp.Close()
			}
			return true
		})
	})
	return nil
}

// Open returns the space named `name`, lazily creating it on first call.
// Subsequent calls return the same handle.
func (s *Service) Open(name string) (kv.KV, error) {
	if s.stopped.Load() {
		return nil, kv.ErrSpaceClosed
	}
	if name == "" {
		return nil, errors.New("kveventual: space name cannot be empty")
	}
	if existing, ok := s.spaces.Load(name); ok {
		return existing.(*space), nil
	}
	sp := newSpace(name, s.cfg.LocalNodeID, s.logger, s.cfg.MetricsCollector)
	sp.wallFloor = s.cfg.WallFloor
	actual, loaded := s.spaces.LoadOrStore(name, sp)
	if loaded {
		// Race lost; return the winner.
		_ = sp.Close()
		return actual.(*space), nil
	}
	s.tel.recordSpaceOpen(name)
	return sp, nil
}

// Spaces returns a snapshot of currently-known space names. Used for
// telemetry and tooling.
func (s *Service) Spaces() []string {
	out := make([]string, 0)
	s.spaces.Range(func(k, _ any) bool {
		out = append(out, k.(string))
		return true
	})
	return out
}

// gcLoop runs periodic tombstone reaping across all spaces.
func (s *Service) gcLoop() {
	t := time.NewTicker(s.cfg.GCPeriod)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			s.runGCOnce()
		}
	}
}

func (s *Service) runGCOnce() {
	now := time.Now()
	// Without per-peer ack tracking (deferred — not needed for v1; the wall
	// floor is sufficient for memory safety), pass an empty safe-counters
	// slice. Tombstones are reaped only via the wall floor.
	var safe []uint64
	s.spaces.Range(func(_, v any) bool {
		sp := v.(*space)
		sp.reapTombstones(safe, now)
		return true
	})
}

// --- membership.UserDelegate implementation ---

// Kind returns the multiplex byte for routing kveventual frames.
func (s *Service) Kind() byte { return DelegateKind }

// GetBroadcasts pulls outgoing delta frames across all spaces. Each frame
// is wrapped with the space name as prefix:
//
//	space_name_len:2 | space_name | crdt frame bytes
//
// `limit` is the total bytes (each emitted frame's len + overhead) the caller
// can transmit this round. Spaces are drained in (random) Range order until
// the budget is consumed; spaces not visited keep their entries pending.
func (s *Service) GetBroadcasts(overhead, limit int) [][]byte {
	if s.stopped.Load() {
		return nil
	}
	var out [][]byte
	totalCost := 0
	s.spaces.Range(func(_, v any) bool {
		sp := v.(*space)
		nameOverhead := 2 + len(sp.name) // space_name_len:2 | space_name

		// Cost of one emitted wrapped frame f = (nameOverhead + len(f)) + overhead.
		// The space's queue accounts for header = overhead+nameOverhead, so
		// asking with that header and the remaining budget yields frames whose
		// total cost matches the caller's view exactly.
		remaining := limit - totalCost
		if remaining <= overhead+nameOverhead {
			return true
		}
		frames := sp.drainBroadcasts(overhead+nameOverhead, remaining)
		for _, f := range frames {
			wrapped := make([]byte, 0, nameOverhead+len(f))
			wrapped = binary.LittleEndian.AppendUint16(wrapped, uint16(len(sp.name)))
			wrapped = append(wrapped, sp.name...)
			wrapped = append(wrapped, f...)
			cost := len(wrapped) + overhead
			if totalCost+cost > limit {
				continue
			}
			totalCost += cost
			out = append(out, wrapped)
		}
		return true
	})
	return out
}

// NotifyMsg delivers an incoming UDP user-broadcast frame to the right space.
func (s *Service) NotifyMsg(payload []byte) {
	if s.stopped.Load() || len(payload) < 2 {
		return
	}
	nameLen := binary.LittleEndian.Uint16(payload[0:2])
	if int(2+nameLen) > len(payload) {
		return
	}
	name := string(payload[2 : 2+nameLen])
	body := payload[2+nameLen:]

	sp, ok := s.spaces.Load(name)
	if !ok {
		// Unknown space — open it lazily so initial deltas at startup
		// (when followers haven't yet `Open`d) are not lost.
		s.spaces.LoadOrStore(name, newSpace(name, s.cfg.LocalNodeID, s.logger, s.cfg.MetricsCollector))
		sp, _ = s.spaces.Load(name)
	}
	sp.(*space).applyFrame(body)
}

// LocalState concatenates per-space digests for outgoing memberlist push/pull.
//
// Wire format:
//
//	n_spaces:2 | repeated{ name_len:2 | name | body_len:4 | body }
//
// `body` is one space's digest+CV blob (the same shape eventualreg uses).
func (s *Service) LocalState(_ bool) []byte {
	if s.stopped.Load() {
		return nil
	}
	type spaceBlob struct {
		name string
		body []byte
	}
	var blobs []spaceBlob
	s.spaces.Range(func(_, v any) bool {
		sp := v.(*space)
		blobs = append(blobs, spaceBlob{name: sp.name, body: sp.localStateBytes()})
		return true
	})

	out := make([]byte, 0, 2+len(blobs)*64)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(blobs)))
	for _, b := range blobs {
		if len(b.name) > 0xFFFF {
			continue
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(len(b.name)))
		out = append(out, b.name...)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(b.body)))
		out = append(out, b.body...)
	}
	return out
}

// MergeRemoteState parses a peer's bulk-transfer payload and updates the
// digest comparison side-effects for each known space.
func (s *Service) MergeRemoteState(buf []byte, _ bool) {
	if s.stopped.Load() || len(buf) < 2 {
		return
	}
	n := binary.LittleEndian.Uint16(buf[0:2])
	off := 2
	for i := uint16(0); i < n; i++ {
		if off+2 > len(buf) {
			return
		}
		nameLen := binary.LittleEndian.Uint16(buf[off : off+2])
		off += 2
		if off+int(nameLen)+4 > len(buf) {
			return
		}
		name := string(buf[off : off+int(nameLen)])
		off += int(nameLen)
		bodyLen := binary.LittleEndian.Uint32(buf[off : off+4])
		off += 4
		if off+int(bodyLen) > len(buf) {
			return
		}
		body := buf[off : off+int(bodyLen)]
		off += int(bodyLen)

		// Decode the digest portion to detect divergence; the CV portion is
		// reserved for tombstone-GC ack tracking when implemented (deferred
		// to v1.1; the wall floor is sufficient for v1 memory safety).
		if len(body) >= crdt.DigestSize {
			remoteDigest, err := crdt.DecodeDigest(body[:crdt.DigestSize])
			if err == nil {
				if v, ok := s.spaces.Load(name); ok {
					sp := v.(*space)
					mismatched := crdt.MakeDigest(sp.state).Diff(remoteDigest)
					if len(mismatched) > 0 {
						s.tel.recordDigestMismatch(name, len(mismatched))
					}
				}
			}
		}
	}
}

// ProviderRegistry adapter. The wider provider in api/kv accepts both raft
// and eventual; this Service implements only the eventual half.
type providerAdapter struct {
	svc *Service
}

func (p *providerAdapter) OpenRaft(_ string) (kv.KV, error) {
	return nil, fmt.Errorf("kveventual: raft mode not provided by this service")
}

func (p *providerAdapter) OpenEventual(name string) (kv.KV, error) {
	return p.svc.Open(name)
}

// Provider returns the eventual half of a ProviderRegistry. Callers compose
// it with kvraft's provider in boot wiring.
func (s *Service) Provider() kv.ProviderRegistry { return &providerAdapter{svc: s} }

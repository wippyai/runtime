// SPDX-License-Identifier: MPL-2.0

package kveventual

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/kv"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/system/crdt"
	"go.uber.org/zap"
)

// space is one named KV namespace backed by an isolated CRDT replica. Spaces
// share the underlying gossip transport (one membership.UserDelegate fans
// out to all spaces by routing on the encoded space prefix in each delta).
type space struct {
	state     *crdt.State
	queue     *crdt.BroadcastQueue
	hub       *watchHub
	logger    *zap.Logger
	collector metrics.Collector
	name      string
	wallFloor time.Duration
	closed    atomic.Bool
}

func newSpace(name, localNode string, logger *zap.Logger, coll metrics.Collector) *space {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &space{
		name:      name,
		state:     crdt.NewState(localNode),
		queue:     crdt.NewBroadcastQueue(localNode, 4096),
		hub:       newWatchHub(name, coll),
		logger:    logger.Named(fmt.Sprintf("kveventual.%s", name)),
		collector: coll,
		wallFloor: 15 * time.Minute,
	}
}

// --- kv.KV implementation ---

func (s *space) Mode() kv.Mode { return kv.ModeEventual }
func (s *space) Name() string  { return s.name }

func (s *space) Get(_ context.Context, key string) (kv.Value, error) {
	if s.closed.Load() {
		return kv.Value{}, kv.ErrSpaceClosed
	}
	e, ok := s.state.LookupEntry(key)
	if !ok {
		return kv.Value{}, kv.ErrKeyNotFound
	}
	return entryToValue(e), nil
}

func (s *space) Put(_ context.Context, key string, data []byte, opts ...kv.PutOption) error {
	if s.closed.Load() {
		return kv.ErrSpaceClosed
	}
	o := kv.CollectPutOptions(opts)
	if o.HasExpectVersion {
		// EVENTUAL has no global ordering — version compare is meaningless.
		// Surface that explicitly rather than silently allowing the write.
		return kv.ErrUnsupported
	}
	if o.ExpectAbsent {
		if _, found := s.state.Lookup(key); found {
			return kv.ErrKeyExists
		}
	}
	e := s.state.Overwrite(key, data, time.Now().UnixMilli())
	s.queue.Push(e)
	s.publish(kv.OpPut, e)
	s.recordOp("put", "ok")
	return nil
}

func (s *space) Delete(_ context.Context, key string) error {
	if s.closed.Load() {
		return kv.ErrSpaceClosed
	}
	tomb, ok := s.state.Unregister(key, time.Now().UnixMilli())
	if !ok {
		// Idempotent: nothing to delete is a non-error.
		s.recordOp("delete", "noop")
		return nil
	}
	s.queue.Push(tomb)
	s.publish(kv.OpDelete, tomb)
	s.recordOp("delete", "ok")
	return nil
}

func (s *space) CompareAndSwap(_ context.Context, _ string, _, _ []byte) error {
	return kv.ErrUnsupported
}

func (s *space) Watch(ctx context.Context, prefix string) (<-chan kv.Event, error) {
	if s.closed.Load() {
		return nil, kv.ErrSpaceClosed
	}
	ch, _ := s.hub.Subscribe(ctx, prefix)
	return ch, nil
}

func (s *space) Scan(_ context.Context, start, end string, fn func(string, kv.Value) bool) error {
	if s.closed.Load() {
		return kv.ErrSpaceClosed
	}
	stop := false
	s.state.Range(start, end, func(e crdt.Entry) bool {
		if stop {
			return false
		}
		if !fn(e.Key, entryToValue(e)) {
			stop = true
			return false
		}
		return true
	})
	return nil
}

func (s *space) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	s.hub.Close()
	return nil
}

// --- gossip integration ---

// drainBroadcasts is called by the Service.Delegate.GetBroadcasts to pull
// pending deltas wrapped with the space name as a prefix.
func (s *space) drainBroadcasts(headerOverhead int) [][]byte {
	frames := s.queue.Drain(headerOverhead)
	for _, f := range frames {
		s.recordBytes("tx", "delta", len(f))
	}
	return frames
}

// applyFrame is called when an inbound delta frame arrives for THIS space.
// It updates the local state and fans out events to watchers.
func (s *space) applyFrame(data []byte) {
	if s.closed.Load() {
		return
	}
	s.recordBytes("rx", "delta", len(data))
	entries, origins, err := crdt.DecodeFrame(data)
	if err != nil {
		s.logger.Debug("malformed frame", zap.Error(err))
	}
	for i := range entries {
		e := entries[i]
		e.Node = s.state.InternNode(origins[i])
		outcome, applied := s.state.Apply(e)
		switch outcome {
		case crdt.MergeApplied, crdt.MergeWallTiebreak, crdt.MergeDeleteWins:
			if applied.Deleted {
				s.publish(kv.OpDelete, applied)
			} else {
				s.publish(kv.OpPut, applied)
			}
		case crdt.MergeNoop:
			// nothing
		}
	}
}

// reapTombstones runs a single GC pass.
func (s *space) reapTombstones(safeByNode []uint64, now time.Time) {
	gcSafe, gcFloor := s.state.ReapTombstones(safeByNode, now.UnixMilli(), s.wallFloor.Milliseconds())
	if gcSafe > 0 || gcFloor > 0 {
		if s.collector != nil {
			s.collector.CounterAdd("kveventual_tombstones_gc_total", float64(gcSafe),
				metrics.Labels{"space": s.name, "reason": "safe_counter"})
			s.collector.CounterAdd("kveventual_tombstones_gc_total", float64(gcFloor),
				metrics.Labels{"space": s.name, "reason": "wall_floor"})
		}
	}
}

// localStateBytes returns the digest+CV blob for memberlist push/pull.
// Format mirrors eventualreg's: digest | cv_len:2 | repeated{ node_len:1 |
// node | counter:8 }.
func (s *space) localStateBytes() []byte {
	digest := crdt.MakeDigest(s.state).Encode()
	cv := s.state.CVSnapshot()

	out := make([]byte, 0, len(digest)+2+len(cv)*16)
	out = append(out, digest...)

	count := 0
	names := make([]string, 0, len(cv))
	for i := 0; i < len(cv); i++ {
		n := s.state.NodeString(uint32(i))
		if n == "" {
			continue
		}
		names = append(names, n)
		count++
	}
	out = append(out, byte(count), byte(count>>8))

	for _, n := range names {
		id := s.state.InternNode(n)
		var counter uint64
		if int(id) < len(cv) {
			counter = cv[id]
		}
		if len(n) > 0xFF {
			n = n[:0xFF]
		}
		out = append(out, byte(len(n)))
		out = append(out, n...)
		var buf8 [8]byte
		writeUint64LE(buf8[:], counter)
		out = append(out, buf8[:]...)
	}
	return out
}

func writeUint64LE(b []byte, v uint64) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	b[4] = byte(v >> 32)
	b[5] = byte(v >> 40)
	b[6] = byte(v >> 48)
	b[7] = byte(v >> 56)
}

// publish converts a crdt.Entry into a kv.Event and fans it out.
func (s *space) publish(op kv.Op, e crdt.Entry) {
	s.hub.Publish(kv.Event{
		Op:    op,
		Key:   e.Key,
		Value: entryToValue(e),
	})
}

func (s *space) recordOp(op, result string) {
	if s.collector == nil {
		return
	}
	s.collector.CounterInc("kveventual_op_total", metrics.Labels{
		"space":  s.name,
		"op":     op,
		"result": result,
	})
}

func (s *space) recordBytes(direction, kind string, n int) {
	if s.collector == nil || n == 0 {
		return
	}
	s.collector.CounterAdd("kveventual_bytes_total", float64(n), metrics.Labels{
		"space": s.name,
		"dir":   direction,
		"kind":  kind,
	})
}

func entryToValue(e crdt.Entry) kv.Value {
	v := kv.Value{
		Data:    e.Value,
		Version: encodeVersion(e.Node, e.Counter),
	}
	return v
}

// encodeVersion packs origin compact id (16 bits) and counter (48 bits) into
// one uint64. Compact IDs above 65535 collide — fine for clusters << 64k nodes.
func encodeVersion(node uint32, counter uint64) uint64 {
	const counterMask uint64 = (1 << 48) - 1
	return (uint64(node&0xFFFF) << 48) | (counter & counterMask)
}

var _ kv.KV = (*space)(nil)
var _ = errors.New

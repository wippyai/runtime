// SPDX-License-Identifier: MPL-2.0

package crdt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// Op identifies a delta kind.
type Op uint8

const (
	// OpPut writes/updates a key.
	OpPut Op = 0
	// OpTombstone deletes a key.
	OpTombstone Op = 1
)

// MaxFrameBytes is the soft cap for one outbound user-broadcast frame.
// Sized to fit comfortably in memberlist's UDP payload budget.
const MaxFrameBytes = 1400

// ErrFrameOverflow indicates a delta would exceed MaxFrameBytes by itself.
// Callers fall through to anti-entropy bulk transfer for that entry.
var ErrFrameOverflow = errors.New("crdt: delta exceeds frame size")

// EncodeDelta writes one entry into the buffer using a compact wire format:
//
//	op:1 | key_len:2 | key | node_str_len:1 | node_str |
//	counter:8 | wall:8 | value_len:4 | value
//
// `originNodeStr` is the canonical string nodeID of the origin so receivers
// can intern without a separate node directory.
func EncodeDelta(buf []byte, e *Entry, originNodeStr string) ([]byte, error) {
	if len(e.Key) > 0xFFFF {
		return buf, fmt.Errorf("crdt: key too long: %d bytes", len(e.Key))
	}
	if len(originNodeStr) > 0xFF {
		return buf, fmt.Errorf("crdt: origin node too long: %d bytes", len(originNodeStr))
	}
	if len(e.Value) > 0xFFFFFFFF {
		return buf, fmt.Errorf("crdt: value too long: %d bytes", len(e.Value))
	}

	op := OpPut
	if e.Deleted {
		op = OpTombstone
	}

	buf = append(buf, byte(op))
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(e.Key)))
	buf = append(buf, e.Key...)
	buf = append(buf, byte(len(originNodeStr)))
	buf = append(buf, originNodeStr...)
	buf = binary.LittleEndian.AppendUint64(buf, e.Counter)
	buf = binary.LittleEndian.AppendUint64(buf, uint64(e.Wall))
	if e.Deleted {
		buf = binary.LittleEndian.AppendUint32(buf, 0)
	} else {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(e.Value)))
		buf = append(buf, e.Value...)
	}
	return buf, nil
}

// DecodeDelta reads one entry from `data`. Returns the entry, the origin
// node string, the number of bytes consumed, and any error.
func DecodeDelta(data []byte) (e Entry, originNode string, n int, err error) {
	r := reader{b: data}
	op := r.byte()
	keyLen := r.u16()
	key := r.str(int(keyLen))
	originLen := r.byte()
	origin := r.str(int(originLen))
	counter := r.u64()
	wall := int64(r.u64())
	valueLen := r.u32()
	value := r.bytes(int(valueLen))

	if r.err != nil {
		return Entry{}, "", 0, r.err
	}

	e = Entry{
		Key:     key,
		Counter: counter,
		Wall:    wall,
		Deleted: Op(op) == OpTombstone,
	}
	if !e.Deleted {
		e.Value = value
	}
	return e, origin, r.pos, nil
}

// DecodeFrame splits a frame into one or more (entry, origin) pairs. Stops
// at end-of-buffer or first decode error; returns whatever it parsed.
func DecodeFrame(data []byte) ([]Entry, []string, error) {
	var entries []Entry
	var origins []string
	for len(data) > 0 {
		e, origin, n, err := DecodeDelta(data)
		if err != nil {
			return entries, origins, err
		}
		entries = append(entries, e)
		origins = append(origins, origin)
		data = data[n:]
	}
	return entries, origins, nil
}

// BroadcastQueue is a goroutine-safe queue of pending deltas. It batches
// outbound entries into frames of ≤ MaxFrameBytes for memberlist
// `GetBroadcasts`. Capacity-bounded so a slow gossip path can't grow
// unbounded under chaos.
type BroadcastQueue struct {
	originNodeStr string
	pending       []Entry
	mu            sync.Mutex
	cap           int
	dropped       uint64
}

// NewBroadcastQueue constructs a queue. `cap` ≤ 0 → 4096.
func NewBroadcastQueue(originNodeStr string, capacity int) *BroadcastQueue {
	if capacity <= 0 {
		capacity = 4096
	}
	return &BroadcastQueue{
		originNodeStr: originNodeStr,
		cap:           capacity,
	}
}

// Push enqueues a delta. Returns false if the queue is full and the entry
// was dropped.
func (q *BroadcastQueue) Push(e Entry) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) >= q.cap {
		q.dropped++
		return false
	}
	q.pending = append(q.pending, e)
	return true
}

// Drain pulls pending deltas, packs them into frames sized to the smaller of
// `MaxFrameBytes - headerOverhead` and the remaining `byteBudget` (minus the
// per-frame header). Entries that don't fit stay queued for the next drain.
// byteBudget <= 0 falls back to a single MaxFrameBytes-sized frame.
//
// The "smaller of" matters: when the multiplex hands a per-delegate share that
// is well below MaxFrameBytes (e.g. 699 bytes when budget is split fairly
// across two delegates), the previous flush threshold of perFrameLimit alone
// meant we'd accumulate ~1385 bytes worth of entries and only THEN try to
// flush — which would always fail the budget check and the entire batch
// would be discarded as "doesn't fit." Result: that delegate never made
// progress.
func (q *BroadcastQueue) Drain(headerOverhead, byteBudget int) [][]byte {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil
	}

	perFrameLimit := MaxFrameBytes - headerOverhead
	if perFrameLimit < 64 {
		perFrameLimit = 64
	}
	if byteBudget <= 0 {
		byteBudget = MaxFrameBytes
	}

	var frames [][]byte
	frame := make([]byte, 0, perFrameLimit)
	bytesEmitted := 0
	var commitIdx []int
	var pendingIdx []int
	var skipIdx []int

	// activeFrameCap is the largest payload the next frame may hold so that
	// flushing it stays within byteBudget. It shrinks as bytesEmitted grows.
	activeFrameCap := func() int {
		remaining := byteBudget - bytesEmitted - headerOverhead
		if remaining <= 0 {
			return 0
		}
		if remaining > perFrameLimit {
			return perFrameLimit
		}
		return remaining
	}

	flushFrame := func() bool {
		if len(frame) == 0 {
			return true
		}
		cost := len(frame) + headerOverhead
		if bytesEmitted+cost > byteBudget {
			return false
		}
		frames = append(frames, frame)
		bytesEmitted += cost
		commitIdx = append(commitIdx, pendingIdx...)
		pendingIdx = pendingIdx[:0]
		frame = make([]byte, 0, perFrameLimit)
		return true
	}

	for i := range q.pending {
		cap := activeFrameCap()
		if cap <= 0 {
			break
		}

		var scratch []byte
		buf, err := EncodeDelta(scratch, &q.pending[i], q.originNodeStr)
		if err != nil {
			skipIdx = append(skipIdx, i)
			continue
		}
		if len(buf) > perFrameLimit {
			// Single delta larger than frame budget — drop it; bulk
			// transfer (digest) will pick it up next anti-entropy round.
			skipIdx = append(skipIdx, i)
			continue
		}
		if len(frame)+len(buf) > cap {
			if !flushFrame() {
				break
			}
			cap = activeFrameCap()
			if cap <= 0 || len(buf) > cap {
				// Entry doesn't fit in remaining budget; leave it queued.
				break
			}
		}
		frame = append(frame, buf...)
		pendingIdx = append(pendingIdx, i)
	}
	flushFrame()

	if len(commitIdx) == 0 && len(skipIdx) == 0 {
		return frames
	}
	remove := make(map[int]struct{}, len(commitIdx)+len(skipIdx))
	for _, i := range commitIdx {
		remove[i] = struct{}{}
	}
	for _, i := range skipIdx {
		remove[i] = struct{}{}
	}
	keep := q.pending[:0]
	for i, e := range q.pending {
		if _, drop := remove[i]; drop {
			continue
		}
		keep = append(keep, e)
	}
	q.pending = keep
	return frames
}

// Dropped returns the lifetime count of dropped entries.
func (q *BroadcastQueue) Dropped() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dropped
}

// Depth returns the current number of pending entries.
func (q *BroadcastQueue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// --- internal helpers ---

type reader struct {
	err error
	b   []byte
	pos int
}

func (r *reader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if r.pos+n > len(r.b) {
		r.err = errors.New("crdt: truncated delta")
		return false
	}
	return true
}

func (r *reader) byte() byte {
	if !r.need(1) {
		return 0
	}
	v := r.b[r.pos]
	r.pos++
	return v
}

func (r *reader) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.b[r.pos:])
	r.pos += 2
	return v
}

func (r *reader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b[r.pos:])
	r.pos += 4
	return v
}

func (r *reader) u64() uint64 {
	if !r.need(8) {
		return 0
	}
	v := binary.LittleEndian.Uint64(r.b[r.pos:])
	r.pos += 8
	return v
}

func (r *reader) str(n int) string {
	if !r.need(n) {
		return ""
	}
	s := string(r.b[r.pos : r.pos+n])
	r.pos += n
	return s
}

func (r *reader) bytes(n int) []byte {
	if !r.need(n) {
		return nil
	}
	out := make([]byte, n)
	copy(out, r.b[r.pos:r.pos+n])
	r.pos += n
	return out
}

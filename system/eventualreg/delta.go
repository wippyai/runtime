// SPDX-License-Identifier: MPL-2.0

package eventualreg

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/pid"
)

// Op identifies the kind of delta on the wire.
type Op uint8

const (
	// OpRegister inserts/updates a name → PID binding.
	OpRegister Op = 0
	// OpUnregister marks a name as a tombstone.
	OpUnregister Op = 1
)

// FrameType is the first byte of every eventualreg frame (inside the
// membership multiplex envelope). Distinguishes the UDP delta stream
// from the targeted shard-pull request/response messages.
type FrameType uint8

const (
	// FrameTypeDelta carries one or more EncodeDelta entries — the
	// existing UDP gossip stream.
	FrameTypeDelta FrameType = 0
	// FrameTypeShardRequest is sent by a peer that detected a digest
	// mismatch and wants the full state of one or more shards.
	FrameTypeShardRequest FrameType = 1
	// FrameTypeShardResponse carries one or more EncodeShardPayload
	// bodies in reply to a FrameTypeShardRequest.
	FrameTypeShardResponse FrameType = 2
)

// EncodeShardRequestFrame builds a complete FrameTypeShardRequest payload
// (including the type byte + sender node header). senderNode is included
// so the receiver knows where to send the response without depending on
// memberlist's sender-identification, which the UserDelegate interface
// does not expose.
//
// Wire format:
//
//	type:1 = FrameTypeShardRequest
//	sender_len:1 | sender
//	shard_request_body  (see EncodeShardRequest)
func EncodeShardRequestFrame(senderNode string, shardIDs []uint16) ([]byte, error) {
	if len(senderNode) > 0xFF {
		return nil, fmt.Errorf("eventualreg: sender node too long: %d", len(senderNode))
	}
	body := EncodeShardRequest(shardIDs)
	out := make([]byte, 0, 2+len(senderNode)+len(body))
	out = append(out, byte(FrameTypeShardRequest))
	out = append(out, byte(len(senderNode)))
	out = append(out, senderNode...)
	out = append(out, body...)
	return out, nil
}

// DecodeShardRequestFrame parses a FrameTypeShardRequest body (the bytes
// AFTER the type byte). Returns the sender's node string and the
// requested shard IDs.
func DecodeShardRequestFrame(data []byte) (senderNode string, shardIDs []uint16, err error) {
	if len(data) < 1 {
		return "", nil, errors.New("eventualreg: shard request frame truncated")
	}
	senderLen := int(data[0])
	if len(data) < 1+senderLen {
		return "", nil, errors.New("eventualreg: shard request sender truncated")
	}
	sender := string(data[1 : 1+senderLen])
	ids, err := DecodeShardRequest(data[1+senderLen:])
	if err != nil {
		return "", nil, err
	}
	return sender, ids, nil
}

// EncodeShardResponseFrame wraps one or more shard payloads (from
// EncodeShardPayload) into a FrameTypeShardResponse frame. The bodies
// are concatenated; each carries its own shard_id + n_entries header so
// the decoder can split.
func EncodeShardResponseFrame(senderNode string, payloads [][]byte) ([]byte, error) {
	if len(senderNode) > 0xFF {
		return nil, fmt.Errorf("eventualreg: sender node too long: %d", len(senderNode))
	}
	total := 2 + len(senderNode)
	for _, p := range payloads {
		total += len(p)
	}
	out := make([]byte, 0, total)
	out = append(out, byte(FrameTypeShardResponse))
	out = append(out, byte(len(senderNode)))
	out = append(out, senderNode...)
	for _, p := range payloads {
		out = append(out, p...)
	}
	return out, nil
}

// DecodeShardResponseFrame parses the body of a FrameTypeShardResponse
// (bytes AFTER the type byte). Returns the sender node and the
// concatenated shard-payload bytes — caller iterates DecodeShardPayload
// on the remainder.
func DecodeShardResponseFrame(data []byte) (senderNode string, payload []byte, err error) {
	if len(data) < 1 {
		return "", nil, errors.New("eventualreg: shard response frame truncated")
	}
	senderLen := int(data[0])
	if len(data) < 1+senderLen {
		return "", nil, errors.New("eventualreg: shard response sender truncated")
	}
	sender := string(data[1 : 1+senderLen])
	return sender, data[1+senderLen:], nil
}

// MaxFrameBytes is a soft cap for one user-broadcast frame. Memberlist
// truncates anything over ~1400 B over UDP — we pack deltas up to this
// limit and start a new frame.
const MaxFrameBytes = 1400

// ErrFrameOverflow indicates a delta would exceed MaxFrameBytes by itself.
// Callers should send the offending entry over TCP instead (digest exchange).
var ErrFrameOverflow = errors.New("eventualreg: delta exceeds frame size")

// EncodeDelta writes one entry into the buffer using a compact format:
//
//	op:1 | name_len:2 | name | node_str_len:1 | node_str |
//	counter:8 | wall:8 | priority:4 | pid_node_len:1 | pid_node |
//	pid_host_len:1 | pid_host | pid_uniq_len:2 | pid_uniq
//
// node_str is the full string nodeID of the origin (so receivers can intern
// without a separate node directory). priority is the cross-origin conflict
// precedence — it MUST travel so every replica resolves conflicts identically.
// PID fields are the three pid.PID substrings, length-prefixed so receivers can
// rebuild without msgpack.
func EncodeDelta(buf []byte, e *Entry, originNodeStr string) ([]byte, error) {
	if len(e.Name) > 0xFFFF {
		return buf, fmt.Errorf("eventualreg: name too long: %d bytes", len(e.Name))
	}
	if len(originNodeStr) > 0xFF {
		return buf, fmt.Errorf("eventualreg: origin node too long: %d bytes", len(originNodeStr))
	}
	if len(e.PID.Node) > 0xFF || len(e.PID.Host) > 0xFF || len(e.PID.UniqID) > 0xFFFF {
		return buf, fmt.Errorf("eventualreg: pid fields too long")
	}

	op := OpRegister
	if e.Deleted {
		op = OpUnregister
	}

	buf = append(buf, byte(op))
	buf = appendUint16(buf, uint16(len(e.Name)))
	buf = append(buf, e.Name...)
	buf = append(buf, byte(len(originNodeStr)))
	buf = append(buf, originNodeStr...)
	buf = appendUint64(buf, e.Counter)
	buf = appendUint64(buf, uint64(e.Wall))
	buf = appendUint32(buf, e.Priority)
	buf = append(buf, byte(len(e.PID.Node)))
	buf = append(buf, e.PID.Node...)
	buf = append(buf, byte(len(e.PID.Host)))
	buf = append(buf, e.PID.Host...)
	buf = appendUint16(buf, uint16(len(e.PID.UniqID)))
	buf = append(buf, e.PID.UniqID...)
	return buf, nil
}

// DecodeDelta reads one entry from `data`, returning the entry, the origin
// nodeID string, and the number of bytes consumed.
func DecodeDelta(data []byte) (e Entry, originNode string, n int, err error) {
	r := reader{b: data}

	op := r.byte()
	nameLen := r.u16()
	name := r.str(int(nameLen))
	originLen := r.byte()
	origin := r.str(int(originLen))
	counter := r.u64()
	wall := int64(r.u64())
	priority := r.u32()
	pidNodeLen := r.byte()
	pidNode := r.str(int(pidNodeLen))
	pidHostLen := r.byte()
	pidHost := r.str(int(pidHostLen))
	pidUniqLen := r.u16()
	pidUniq := r.str(int(pidUniqLen))

	if r.err != nil {
		return Entry{}, "", 0, r.err
	}

	e = Entry{
		Name:     name,
		Counter:  counter,
		Wall:     wall,
		Priority: priority,
		Deleted:  Op(op) == OpUnregister,
	}
	if !e.Deleted {
		e.PID = pid.PID{Node: pidNode, Host: pidHost, UniqID: pidUniq}
	}
	return e, origin, r.pos, nil
}

// BroadcastQueue is a goroutine-safe queue of pending deltas. It batches
// outbound entries into frames of ≤ MaxFrameBytes for memberlist
// `GetBroadcasts`. Capacity is bounded so a slow gossip path can't grow
// unbounded under chaos.
type BroadcastQueue struct {
	originNodeStr string
	// originOf resolves an entry's true origin string from its compact Node ID.
	// Foreign-origin entries (e.g. a reaped departed node's tombstone) MUST
	// gossip with their own origin so peers fold them onto the same dot via
	// delete-wins, not onto a fresh local-origin dot. Falls back to
	// originNodeStr when nil or when the lookup yields "".
	originOf func(node uint32) string
	pending  []*Entry
	mu       sync.Mutex
	cap      int
	dropped       uint64
}

// NewBroadcastQueue constructs a queue. `cap` is the max number of pending
// entries before new ones are dropped (telemetry surfaces the count).
// `originOf` resolves an entry's origin string from its compact Node ID so
// foreign-origin entries gossip under their true origin; nil falls back to
// originNodeStr for every entry.
func NewBroadcastQueue(originNodeStr string, capacity int, originOf func(node uint32) string) *BroadcastQueue {
	if capacity <= 0 {
		capacity = 4096
	}
	return &BroadcastQueue{
		originNodeStr: originNodeStr,
		originOf:      originOf,
		cap:           capacity,
	}
}

// originFor returns the wire origin string for an entry: its true origin via
// originOf when resolvable, else the queue's local origin.
func (q *BroadcastQueue) originFor(e *Entry) string {
	if q.originOf != nil {
		if s := q.originOf(e.Node); s != "" {
			return s
		}
	}
	return q.originNodeStr
}

// Push enqueues a delta. Returns false if the queue is full and the entry
// was dropped (callers can increment a metric).
func (q *BroadcastQueue) Push(e *Entry) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) >= q.cap {
		q.dropped++
		return false
	}
	q.pending = append(q.pending, e)
	return true
}

// Drain pulls deltas, packs them into frames sized to the smaller of
// `MaxFrameBytes - headerOverhead` and the remaining `byteBudget` (minus the
// per-frame header). Entries that don't fit stay queued for the next drain.
// byteBudget <= 0 falls back to a single MaxFrameBytes-sized frame.
//
// See system/crdt/delta.go for the full reasoning behind activeFrameCap. In
// short: when the outer multiplex hands a per-delegate share well below
// MaxFrameBytes, flushing only at perFrameLimit means we'd accumulate ~1385
// bytes and then fail the budget check on flush — emitting nothing while
// the queue silently grows.
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
	frame := make([]byte, 1, perFrameLimit)
	frame[0] = byte(FrameTypeDelta) // every delta frame is type-tagged
	bytesEmitted := 0
	var commitIdx []int
	var pendingIdx []int
	var skipIdx []int

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
		// frame always carries the leading type byte; treat as empty
		// when no entries have been appended past it.
		if len(frame) <= 1 {
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
		frame = make([]byte, 1, perFrameLimit)
		frame[0] = byte(FrameTypeDelta)
		return true
	}

	for i, e := range q.pending {
		cap := activeFrameCap()
		if cap <= 0 {
			break
		}

		var scratch []byte
		buf, err := EncodeDelta(scratch, e, q.originFor(e))
		if err != nil {
			skipIdx = append(skipIdx, i)
			continue
		}
		if len(buf) > perFrameLimit {
			skipIdx = append(skipIdx, i)
			continue
		}
		if len(frame)+len(buf) > cap {
			if !flushFrame() {
				break
			}
			cap = activeFrameCap()
			if cap <= 0 || len(buf) > cap {
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

// Dropped returns the lifetime count of dropped entries (queue full).
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

// DecodeFrame splits a frame into one or more (entry, originNode) pairs.
// Stops at end-of-buffer or first decode error; returns whatever it managed
// to parse plus the trailing error (if any).
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

// --- internal helpers ---

func appendUint16(b []byte, v uint16) []byte {
	return binary.LittleEndian.AppendUint16(b, v)
}

func appendUint32(b []byte, v uint32) []byte {
	return binary.LittleEndian.AppendUint32(b, v)
}

func appendUint64(b []byte, v uint64) []byte {
	return binary.LittleEndian.AppendUint64(b, v)
}

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
		r.err = errors.New("eventualreg: truncated delta")
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

// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// digestVersion is the wire-format version. Bump if encoding changes.
const digestVersion uint8 = 1

// DigestSize is the byte size of one full digest frame: header + 64 entries.
//
//	header:  ver:1 | shard_count:2
//	per shard: shard_id:2 | xxhash64:8
const DigestSize = 1 + 2 + ShardCount*(2+8)

// Digest is a snapshot of per-shard state hashes for anti-entropy.
type Digest struct {
	Hashes [ShardCount]uint64
}

// MakeDigest reads each shard's hash from the State.
func MakeDigest(s *State) Digest {
	var d Digest
	for i := 0; i < ShardCount; i++ {
		d.Hashes[i] = s.ShardHash(i)
	}
	return d
}

// Encode serializes the digest to bytes. Always returns DigestSize bytes.
func (d Digest) Encode() []byte {
	out := make([]byte, 0, DigestSize)
	out = append(out, digestVersion)
	out = binary.LittleEndian.AppendUint16(out, uint16(ShardCount))
	for i, h := range d.Hashes {
		out = binary.LittleEndian.AppendUint16(out, uint16(i))
		out = binary.LittleEndian.AppendUint64(out, h)
	}
	return out
}

// DecodeDigest reads a digest from bytes.
func DecodeDigest(data []byte) (Digest, error) {
	var d Digest
	if len(data) < 3 {
		return d, errors.New("eventualreg: digest header truncated")
	}
	ver := data[0]
	if ver != digestVersion {
		return d, fmt.Errorf("eventualreg: unsupported digest version: %d", ver)
	}
	count := binary.LittleEndian.Uint16(data[1:3])
	if int(count) != ShardCount {
		return d, fmt.Errorf("eventualreg: digest shard count mismatch: got %d want %d", count, ShardCount)
	}
	if len(data) < DigestSize {
		return d, errors.New("eventualreg: digest body truncated")
	}
	off := 3
	for j := 0; j < ShardCount; j++ {
		shardID := binary.LittleEndian.Uint16(data[off : off+2])
		off += 2
		hash := binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		if int(shardID) >= ShardCount {
			return d, fmt.Errorf("eventualreg: digest shard id out of range: %d", shardID)
		}
		d.Hashes[shardID] = hash
	}
	return d, nil
}

// Diff returns the shard IDs whose hashes differ between local and remote.
// Returns at most ShardCount entries, sorted ascending.
func (d Digest) Diff(remote Digest) []uint16 {
	var out []uint16
	for i := 0; i < ShardCount; i++ {
		if d.Hashes[i] != remote.Hashes[i] {
			out = append(out, uint16(i))
		}
	}
	return out
}

// ShardPayload is the bulk-transfer body for a single shard: a header with the
// shard ID and entry count, followed by entries in delta wire format.
type ShardPayload struct {
	Entries []Entry
	Origins []string // 1:1 with Entries — origin nodeID strings
	ShardID uint16
}

// EncodeShardPayload serializes one shard's full state for anti-entropy.
//
// Wire format:
//
//	shard_id:2 | n_entries:4 | { delta_record × n }
func EncodeShardPayload(buf []byte, shardID uint16, entries []*Entry, originLookup func(uint32) string) ([]byte, error) {
	buf = binary.LittleEndian.AppendUint16(buf, shardID)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(entries)))
	for _, e := range entries {
		origin := originLookup(e.Node)
		var err error
		buf, err = EncodeDelta(buf, e, origin)
		if err != nil {
			return buf, err
		}
	}
	return buf, nil
}

// EncodeShardPayloadsBounded serializes a shard into one or more shard payloads,
// each at most maxBytes. Chunking at entry boundaries keeps the targeted
// anti-entropy path useful for large shards without allowing a single response
// to become an unbounded reliable-message allocation. Entries that cannot fit by
// themselves are skipped.
func EncodeShardPayloadsBounded(shardID uint16, entries []*Entry, originLookup func(uint32) string, maxBytes int) (payloads [][]byte, skipped int, err error) {
	const headerLen = 6
	if maxBytes <= headerLen {
		return nil, len(entries), nil
	}

	buf := make([]byte, 0, maxBytes)
	startPayload := func() {
		buf = binary.LittleEndian.AppendUint16(buf[:0], shardID)
		buf = binary.LittleEndian.AppendUint32(buf, 0)
	}
	count := uint32(0)
	startPayload()

	flush := func() {
		if count == 0 {
			return
		}
		binary.LittleEndian.PutUint32(buf[2:6], count)
		payloads = append(payloads, append([]byte(nil), buf...))
		count = 0
		startPayload()
	}

	for _, e := range entries {
		origin := originLookup(e.Node)
		recLen, err := encodedDeltaLen(e, origin)
		if err != nil {
			return nil, 0, err
		}
		if recLen+headerLen > maxBytes {
			skipped++
			continue
		}
		if len(buf)+recLen > maxBytes {
			flush()
		}
		buf, err = EncodeDelta(buf, e, origin)
		if err != nil {
			return nil, 0, err
		}
		count++
	}
	flush()
	return payloads, skipped, nil
}

func encodedDeltaLen(e *Entry, originNodeStr string) (int, error) {
	if len(e.Name) > 0xFFFF {
		return 0, fmt.Errorf("eventualreg: name too long: %d bytes", len(e.Name))
	}
	if len(originNodeStr) > 0xFF {
		return 0, fmt.Errorf("eventualreg: origin node too long: %d bytes", len(originNodeStr))
	}
	if len(e.PID.Node) > 0xFF || len(e.PID.Host) > 0xFF || len(e.PID.UniqID) > 0xFFFF {
		return 0, fmt.Errorf("eventualreg: pid fields too long")
	}
	return 1 + 2 + len(e.Name) + 1 + len(originNodeStr) + 8 + 8 + 4 +
		1 + len(e.PID.Node) + 1 + len(e.PID.Host) + 2 + len(e.PID.UniqID), nil
}

// DecodeShardPayload parses a shard bulk-transfer body.
func DecodeShardPayload(data []byte) (ShardPayload, int, error) {
	if len(data) < 6 {
		return ShardPayload{}, 0, errors.New("eventualreg: shard payload header truncated")
	}
	shardID := binary.LittleEndian.Uint16(data[0:2])
	n := binary.LittleEndian.Uint32(data[2:6])
	if shardID >= ShardCount {
		return ShardPayload{}, 0, fmt.Errorf("eventualreg: shard id out of range: %d", shardID)
	}
	off := 6
	entries := make([]Entry, 0, n)
	origins := make([]string, 0, n)
	for j := uint32(0); j < n; j++ {
		e, origin, consumed, err := DecodeDelta(data[off:])
		if err != nil {
			return ShardPayload{}, 0, err
		}
		entries = append(entries, e)
		origins = append(origins, origin)
		off += consumed
	}
	return ShardPayload{ShardID: shardID, Entries: entries, Origins: origins}, off, nil
}

// EncodeShardRequest packs a list of shard IDs the requester wants from the peer.
//
//	n:2 | shard_id × n
func EncodeShardRequest(ids []uint16) []byte {
	buf := make([]byte, 0, 2+2*len(ids))
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(ids)))
	for _, id := range ids {
		buf = binary.LittleEndian.AppendUint16(buf, id)
	}
	return buf
}

// DecodeShardRequest is the inverse.
func DecodeShardRequest(data []byte) ([]uint16, error) {
	if len(data) < 2 {
		return nil, errors.New("eventualreg: shard request header truncated")
	}
	n := binary.LittleEndian.Uint16(data[0:2])
	if len(data) < int(2+2*n) {
		return nil, errors.New("eventualreg: shard request body truncated")
	}
	out := make([]uint16, n)
	off := 2
	for i := uint16(0); i < n; i++ {
		id := binary.LittleEndian.Uint16(data[off : off+2])
		if id >= ShardCount {
			return nil, fmt.Errorf("eventualreg: shard request out-of-range id: %d", id)
		}
		out[i] = id
		off += 2
	}
	return out, nil
}

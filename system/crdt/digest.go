// SPDX-License-Identifier: MPL-2.0

package crdt

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const digestVersion uint8 = 1

// DigestSize is the byte size of one full digest frame.
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

// Encode serializes the digest. Always returns DigestSize bytes.
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
		return d, errors.New("crdt: digest header truncated")
	}
	if data[0] != digestVersion {
		return d, fmt.Errorf("crdt: unsupported digest version: %d", data[0])
	}
	count := binary.LittleEndian.Uint16(data[1:3])
	if int(count) != ShardCount {
		return d, fmt.Errorf("crdt: digest shard count mismatch: got %d want %d", count, ShardCount)
	}
	if len(data) < DigestSize {
		return d, errors.New("crdt: digest body truncated")
	}
	off := 3
	for j := 0; j < ShardCount; j++ {
		shardID := binary.LittleEndian.Uint16(data[off : off+2])
		off += 2
		hash := binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		if int(shardID) >= ShardCount {
			return d, fmt.Errorf("crdt: digest shard id out of range: %d", shardID)
		}
		d.Hashes[shardID] = hash
	}
	return d, nil
}

// Diff returns the shard IDs whose hashes differ between local and remote,
// sorted ascending.
func (d Digest) Diff(remote Digest) []uint16 {
	var out []uint16
	for i := 0; i < ShardCount; i++ {
		if d.Hashes[i] != remote.Hashes[i] {
			out = append(out, uint16(i))
		}
	}
	return out
}

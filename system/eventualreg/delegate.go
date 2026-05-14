// SPDX-License-Identifier: MPL-2.0

package eventualreg

import (
	"encoding/binary"
	"time"

	"go.uber.org/zap"
)

// DelegateKind is the multiplex byte that membership uses to route eventualreg
// frames. It must be unique across all UserDelegates registered on a node.
const DelegateKind byte = 0xE1 // 'e'-ventual

// Delegate plugs a Service into membership.UserDelegate.
type Delegate struct {
	svc    *Service
	logger *zap.Logger
}

// NewDelegate constructs a delegate that routes membership frames to svc.
func NewDelegate(svc *Service, logger *zap.Logger) *Delegate {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Delegate{svc: svc, logger: logger.Named("eventualreg.delegate")}
}

// Kind returns the multiplex byte for eventualreg frames.
func (d *Delegate) Kind() byte { return DelegateKind }

// GetBroadcasts pulls outgoing delta frames. `limit` caps total bytes
// (frame body + overhead) we can emit this gossip cycle; remainder stays
// queued for next cycle.
func (d *Delegate) GetBroadcasts(overhead, limit int) [][]byte {
	return d.svc.DrainBroadcasts(overhead, limit)
}

// NotifyMsg handles one incoming UDP delta frame.
func (d *Delegate) NotifyMsg(payload []byte) {
	d.svc.OnFrame(payload)
}

// LocalState returns the bulk-transfer payload for an outgoing memberlist
// push/pull. Wire format:
//
//	digest:DigestSize | cv_len:2 | repeated{ node_len:1 | node | counter:8 }
//
// The peer receives both pieces in one push/pull and uses the digest to
// detect divergence; the CV unblocks tombstone GC.
func (d *Delegate) LocalState(_ bool) []byte {
	digest := d.svc.LocalDigest().Encode()

	cv := d.svc.CVSnapshot()
	stringIDs := d.collectStringIDs(len(cv))

	out := make([]byte, 0, len(digest)+2+len(cv)*16)
	out = append(out, digest...)

	// CV count: write the actual interned-node count, not len(cv) (the
	// internal slice may be over-allocated).
	count := uint16(len(stringIDs))
	out = binary.LittleEndian.AppendUint16(out, count)
	for i := uint16(0); i < count; i++ {
		name := stringIDs[i]
		if len(name) > 0xFF {
			name = name[:0xFF]
		}
		out = append(out, byte(len(name)))
		out = append(out, name...)
		out = binary.LittleEndian.AppendUint64(out, cv[i])
	}

	// No appended shard payload by default — receivers request shards
	// separately by mismatched-digest negotiation. The current memberlist
	// integration is push-only for state, which converges in O(log N) ticks
	// without explicit pull because both sides keep shipping their LocalState.
	return out
}

// MergeRemoteState applies a peer's bulk-transfer payload.
func (d *Delegate) MergeRemoteState(buf []byte, _ bool) {
	start := time.Now()

	if len(buf) < DigestSize {
		d.logger.Debug("eventualreg: short remote state", zap.Int("len", len(buf)))
		return
	}

	digest, err := DecodeDigest(buf[:DigestSize])
	if err != nil {
		d.logger.Debug("eventualreg: digest decode error", zap.Error(err))
		return
	}
	off := DigestSize

	if len(buf) < off+2 {
		return
	}
	count := binary.LittleEndian.Uint16(buf[off : off+2])
	off += 2

	peerNode := ""
	peerCV := make([]uint64, count)
	for i := uint16(0); i < count; i++ {
		if off+1 > len(buf) {
			return
		}
		nameLen := int(buf[off])
		off++
		if off+nameLen+8 > len(buf) {
			return
		}
		name := string(buf[off : off+nameLen])
		off += nameLen
		counter := binary.LittleEndian.Uint64(buf[off : off+8])
		off += 8
		// Reindex by local compact ID — interns the peer name if needed.
		localID := d.svc.state.internNode(name)
		if int(localID) >= len(peerCV) {
			grow := make([]uint64, localID+1)
			copy(grow, peerCV)
			peerCV = grow
		}
		peerCV[localID] = counter
		// Heuristic: the peer's locally-highest counter is likely from itself —
		// we can't be certain, but we record peerCV regardless and forget on leave.
		if peerNode == "" || counter > peerCV[d.svc.state.internNode(peerNode)] {
			peerNode = name
		}
	}

	// Compare digests; on mismatch, ask the peer for the divergent
	// shards over reliable TCP. The push-only LocalState body carries
	// only the digest+CV, so when the broadcast queue overflows under
	// chaos there is otherwise no recovery channel — RequestShards
	// closes that gap. Rate-limited inside the service.
	local := d.svc.LocalDigest()
	mismatched := local.Diff(digest)

	d.svc.OnPeerDigest(peerNode, peerCV)
	if len(mismatched) > 0 {
		d.logger.Debug("eventualreg: digest mismatch",
			zap.String("peer", peerNode), zap.Int("shards", len(mismatched)))
		if peerNode != "" {
			d.svc.RequestShards(peerNode, mismatched)
		}
	}

	d.svc.tel.recordAntiEntropy("ok", float64(time.Since(start).Milliseconds()), len(mismatched))
}

// collectStringIDs returns up to `n` interned node strings in compact-ID
// order. Holds the State cv mutex briefly.
func (d *Delegate) collectStringIDs(n int) []string {
	out := make([]string, 0, n)
	d.svc.state.cvMu.RLock()
	for i := 0; i < n && i < len(d.svc.state.stringIDs); i++ {
		out = append(out, d.svc.state.stringIDs[i])
	}
	d.svc.state.cvMu.RUnlock()
	return out
}

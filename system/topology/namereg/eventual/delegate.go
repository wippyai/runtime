// SPDX-License-Identifier: MPL-2.0

package eventual

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
	return &Delegate{svc: svc, logger: logger.Named("eventual.delegate")}
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
//	digest:DigestSize | cv_len:2 | repeated{ node_len:1 | node | counter:8 } | node_len:1 | node
//
// The peer receives both pieces in one push/pull and uses the digest to
// detect divergence; the CV unblocks tombstone GC. The trailing node id is the
// authoritative sender so the receiver can target shard-pull requests.
func (d *Delegate) LocalState(_ bool) []byte {
	digest := d.svc.LocalDigest().Encode()

	cv := d.svc.CVSnapshot()
	pairs := d.collectCVPairs(cv)

	out := make([]byte, 0, len(digest)+2+len(pairs)*16)
	out = append(out, digest...)

	// CV count: the number of (name, counter) pairs emitted. Reclaimed slots
	// (empty string id) are skipped — the wire carries origin strings, so a
	// peer re-interns by name and the local compact index is never sent.
	out = binary.LittleEndian.AppendUint16(out, uint16(len(pairs)))
	for _, p := range pairs {
		name := p.name
		if len(name) > 0xFF {
			name = name[:0xFF]
		}
		out = append(out, byte(len(name)))
		out = append(out, name...)
		out = binary.LittleEndian.AppendUint64(out, p.counter)
	}

	// Append our node id so the receiver can target shard-pull requests at the
	// actual sender instead of guessing from the CV. Trailing bytes: older peers
	// stop after the CV pairs and ignore this; newer peers read it.
	id := d.svc.cfg.LocalNodeID
	if len(id) > 0xFF {
		id = id[:0xFF]
	}
	out = append(out, byte(len(id)))
	out = append(out, id...)

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
		// Fallback when the sender did not append its id (older peer): the
		// locally-highest counter is the best guess for the sender.
		if peerNode == "" || counter > peerCV[d.svc.state.internNode(peerNode)] {
			peerNode = name
		}
	}

	// Authoritative sender id appended by LocalState, when present.
	if off < len(buf) {
		nameLen := int(buf[off])
		off++
		if off+nameLen <= len(buf) {
			peerNode = string(buf[off : off+nameLen])
		}
	}

	// Compare digests; on mismatch, ask the sender for the divergent shards over
	// reliable TCP. RequestShards is the recovery channel when the one-shot
	// broadcast queue overflows under chaos. Rate-limited inside the service.
	local := d.svc.LocalDigest()
	mismatched := local.Diff(digest)

	d.svc.OnPeerDigest(peerNode, peerCV)
	if len(mismatched) > 0 {
		d.logger.Debug("eventualreg: digest mismatch",
			zap.String("peer", peerNode), zap.Int("shards", len(mismatched)))
		if peerNode != "" && peerNode != d.svc.cfg.LocalNodeID {
			d.svc.RequestShards(peerNode, mismatched)
		}
	}

	d.svc.tel.recordAntiEntropy("ok", float64(time.Since(start).Milliseconds()), len(mismatched))
}

// cvPair is one (origin string, counter) the LocalState body carries.
type cvPair struct {
	name    string
	counter uint64
}

// collectCVPairs returns the interned (origin string, cv counter) pairs to put
// on the wire, skipping reclaimed slots (empty string id). Holds the State cv
// mutex briefly. cv is indexed by compact id; the pair carries the string so
// the peer re-interns by name without depending on our local index.
func (d *Delegate) collectCVPairs(cv []uint64) []cvPair {
	d.svc.state.cvMu.RLock()
	defer d.svc.state.cvMu.RUnlock()
	out := make([]cvPair, 0, len(cv))
	for i := 0; i < len(cv) && i < len(d.svc.state.stringIDs); i++ {
		name := d.svc.state.stringIDs[i]
		if name == "" {
			continue
		}
		out = append(out, cvPair{name: name, counter: cv[i]})
	}
	return out
}

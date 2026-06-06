// SPDX-License-Identifier: MPL-2.0

package kv

// CRDTDelegateKind is the membership multiplex byte for store.kv.crdt gossip
// frames. It must be unique across all UserDelegates registered on a node
// (distinct from eventualreg's 0xE1).
const CRDTDelegateKind byte = 0xD2

// CRDTDelegate plugs a CRDTEngine into membership.UserDelegate: UDP broadcasts
// carry deltas, TCP push/pull carries the full state for anti-entropy.
type CRDTDelegate struct {
	engine *CRDTEngine
}

// NewCRDTDelegate wraps the engine.
func NewCRDTDelegate(engine *CRDTEngine) *CRDTDelegate {
	return &CRDTDelegate{engine: engine}
}

// Kind returns the multiplex byte.
func (d *CRDTDelegate) Kind() byte { return CRDTDelegateKind }

// GetBroadcasts drains pending deltas for this gossip cycle.
func (d *CRDTDelegate) GetBroadcasts(overhead, limit int) [][]byte {
	return d.engine.DrainBroadcasts(overhead, limit)
}

// NotifyMsg applies one incoming delta frame.
func (d *CRDTDelegate) NotifyMsg(payload []byte) { d.engine.OnFrame(payload) }

// LocalState returns the full state for push/pull bulk sync.
func (d *CRDTDelegate) LocalState(_ bool) []byte { return d.engine.FullState() }

// MergeRemoteState applies a peer's full-state push/pull payload.
func (d *CRDTDelegate) MergeRemoteState(buf []byte, _ bool) {
	if len(buf) > 0 {
		if !d.engine.mergeFullState(buf) {
			d.engine.OnFrame(buf)
		}
	}
}

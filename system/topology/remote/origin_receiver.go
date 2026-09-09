// SPDX-License-Identifier: MPL-2.0

package remote

import "github.com/wippyai/runtime/api/relay"

// OriginReceiver admits target receipts and completion through Outbound before
// the bounded local delivery callback can notify a watcher. Register it on the
// native topology host using owned host registration. Close its admission before
// releasing that registration; registration release alone does not drain calls.
// The destination-side request/local-observation receiver is a separate role.
type OriginReceiver struct{ outbound *Outbound }

func NewOriginReceiver(outbound *Outbound) (*OriginReceiver, error) {
	if outbound == nil {
		return nil, ErrMonitorExpectation
	}
	return &OriginReceiver{outbound: outbound}, nil
}

// Send transfers and releases pkg only on successful admission. Rejection leaves
// ownership with the caller, matching native relay's transactional contract.
// No network I/O runs here; Outbound's local queue callback must stay bounded.
func (r *OriginReceiver) Send(pkg *relay.Package) error {
	if err := r.outbound.Apply(pkg); err != nil {
		return err
	}
	relay.ReleasePackage(pkg)
	return nil
}

// Close fences and joins admitted callbacks. Calls dispatched before host
// unregistration still see Outbound closed and cannot deliver another EXIT.
func (r *OriginReceiver) Close() { r.outbound.Close() }

var _ relay.Receiver = (*OriginReceiver)(nil)

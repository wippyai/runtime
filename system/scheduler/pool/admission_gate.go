// SPDX-License-Identifier: MPL-2.0

package pool

import (
	"sync"
	"sync/atomic"
)

const admissionClosedBit uint64 = 1 << 63

// AdmissionGate provides a low-overhead linearization point between Call and
// Stop. Begin is one uncontended CAS on the hot path; Stop closes admission and
// waits until all already-admitted calls have ended.
type AdmissionGate struct {
	drained chan struct{}
	state   atomic.Uint64
	once    sync.Once
}

// NewAdmissionGate creates an open admission gate.
func NewAdmissionGate() *AdmissionGate {
	return &AdmissionGate{drained: make(chan struct{})}
}

// Begin admits one call unless Stop has already closed the gate.
func (g *AdmissionGate) Begin() bool {
	for {
		state := g.state.Load()
		if state&admissionClosedBit != 0 {
			return false
		}
		if g.state.CompareAndSwap(state, state+1) {
			return true
		}
	}
}

// End marks one admitted call complete.
func (g *AdmissionGate) End() {
	if g.state.Add(^uint64(0)) == admissionClosedBit {
		g.closeDrained()
	}
}

// Stop closes admission and blocks until all admitted calls have ended.
func (g *AdmissionGate) Stop() {
	for {
		state := g.state.Load()
		if state&admissionClosedBit != 0 {
			break
		}
		if g.state.CompareAndSwap(state, state|admissionClosedBit) {
			if state == 0 {
				g.closeDrained()
			}
			break
		}
	}
	<-g.drained
}

func (g *AdmissionGate) closeDrained() {
	g.once.Do(func() {
		close(g.drained)
	})
}

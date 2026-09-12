// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"fmt"
	"sync"
)

// outboundBudget accounts retained payload bytes and entry overhead separately.
// A reservation follows ownership through admission, queue, writer and requeue;
// only its final owner releases the retained capacity.
type outboundBudget struct {
	mu                   sync.Mutex
	parent               *outboundBudget
	root                 *outboundBudget
	maxEntries, maxBytes uint64
	entries, bytes       uint64
	// Protected capacity is carved out of the total, never additional memory.
	protectedEntries, protectedBytes uint64
	ordinaryEntries, ordinaryBytes   uint64
	changed                          chan struct{}
	waiters                          int
	closed                           bool // guarded by root.mu; invalidates future admission only
}

type outboundReservation struct {
	budget   *outboundBudget
	bytes    uint64
	control  bool
	released bool // guarded by budget.root.mu
}

func newOutboundBudget(entries, bytes uint64) (*outboundBudget, error) {
	if entries == 0 || bytes == 0 {
		return nil, fmt.Errorf("outbound budget requires positive entry and byte limits")
	}
	b := &outboundBudget{maxEntries: entries, maxBytes: bytes, changed: make(chan struct{})}
	b.root = b
	return b, nil
}

// child creates an immutable accounting scope. All ancestors are charged under
// one root lock: waiting for a peer never hoards aggregate capacity. Limits may
// model aggregate, traffic class and peer scopes without changing frame ownership.
func (b *outboundBudget) child(entries, bytes uint64) (*outboundBudget, error) {
	if entries == 0 || bytes == 0 {
		return nil, fmt.Errorf("outbound budget requires positive entry and byte limits")
	}
	return &outboundBudget{parent: b, root: b.root, maxEntries: entries, maxBytes: bytes}, nil
}

// protect configures an unpublished scope. Control may borrow ordinary capacity,
// but ordinary frames cannot consume the protected portion. Limits are immutable
// once the scope is exposed to concurrent admission.
func (b *outboundBudget) protect(entries, bytes uint64) error {
	if entries >= b.maxEntries || bytes >= b.maxBytes {
		return fmt.Errorf("outbound protection must leave ordinary entry and byte capacity")
	}
	b.protectedEntries, b.protectedBytes = entries, bytes
	return nil
}

// reserve waits only before ownership transfer. Cancellation cannot retract an
// accepted reservation. Callers must dispose it if later enqueue admission fails.
func (b *outboundBudget) reserve(ctx context.Context, bytes uint64, wait bool) (*outboundReservation, error) {
	return b.reserveTraffic(ctx, bytes, wait, false)
}

// reserveTraffic keeps aggregate, peer and generation accounting atomic. Control
// is a scheduling property chosen by the native sender, not a wire class or a
// claim supplied by a remote peer.
func (b *outboundBudget) reserveTraffic(ctx context.Context, bytes uint64, wait, control bool) (*outboundReservation, error) {
	if ctx == nil {
		return nil, fmt.Errorf("outbound reservation requires context")
	}
	for scope := b; scope != nil; scope = scope.parent {
		limit := scope.maxBytes
		if !control {
			limit -= scope.protectedBytes
		}
		if bytes > limit {
			return nil, fmt.Errorf("outbound frame exceeds retained-byte budget")
		}
	}
	root := b.root
	for {
		root.mu.Lock()
		if err := ctx.Err(); err != nil {
			root.mu.Unlock()
			return nil, err
		}
		available := true
		for scope := b; scope != nil; scope = scope.parent {
			if scope.closed {
				root.mu.Unlock()
				return nil, ErrNodeNotManaged
			}
			if scope.entries >= scope.maxEntries || bytes > scope.maxBytes-scope.bytes {
				available = false
			}
			if !control && (scope.ordinaryEntries >= scope.maxEntries-scope.protectedEntries || bytes > scope.maxBytes-scope.protectedBytes-scope.ordinaryBytes) {
				available = false
			}
		}
		if available {
			for scope := b; scope != nil; scope = scope.parent {
				scope.entries++
				scope.bytes += bytes
				if !control {
					scope.ordinaryEntries++
					scope.ordinaryBytes += bytes
				}
			}
			root.mu.Unlock()
			return &outboundReservation{budget: b, bytes: bytes, control: control}, nil
		}
		changed := root.changed
		if wait {
			root.waiters++
		}
		root.mu.Unlock()
		if !wait {
			return nil, ErrQueueFull
		}
		select {
		case <-ctx.Done():
		case <-changed:
		}
		root.mu.Lock()
		root.waiters--
		root.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}

// close stops admission through this scope and its descendants. It does not
// release accepted work: queued and in-flight frames retain their credits until
// their actual owner disposes them. A replacement peer gets a fresh child scope.
func (b *outboundBudget) close() {
	root := b.root
	root.mu.Lock()
	defer root.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	root.wakeLocked()
}

func (b *outboundBudget) wakeLocked() {
	if b.waiters != 0 {
		close(b.changed)
		b.changed = make(chan struct{})
	}
}

func (r *outboundReservation) release() {
	if r == nil {
		return
	}
	b := r.budget
	root := b.root
	root.mu.Lock()
	defer root.mu.Unlock()
	if r.released {
		return
	}
	r.released = true
	for scope := b; scope != nil; scope = scope.parent {
		scope.entries--
		scope.bytes -= r.bytes
		if !r.control {
			scope.ordinaryEntries--
			scope.ordinaryBytes -= r.bytes
		}
	}
	root.wakeLocked()
}

func releaseOutbound(frames []Outbound) {
	for _, frame := range frames {
		frame.reservation.release()
	}
}

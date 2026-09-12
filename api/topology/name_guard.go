// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	"errors"
	"sync"
)

// NameGuard serializes admission decisions for the same name across the local
// naming scopes. Readers do not acquire it. Share one instance across a node's
// registries; this is local coordination, not a distributed ownership lock.
// Entries exist only while held or awaited, so idle names retain no storage.
// The zero value is ready to use. A nil guard disables coordination.
var ErrNameAdmissionClosed = errors.New("name admission closed")

type NameGuard struct {
	closed  bool
	closing chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	names   map[string]*nameAdmission
}

type nameAdmission struct {
	available chan struct{}
	refs      int
}

// LockContext waits for admission of name. The returned release function must
// be called exactly once. Cancellation never transfers ownership to the caller.
func (g *NameGuard) LockContext(ctx context.Context, name string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g == nil {
		return func() {}, nil
	}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil, ErrNameAdmissionClosed
	}
	if g.closing == nil {
		g.closing = make(chan struct{})
		g.done = make(chan struct{})
	}
	closing := g.closing
	if g.names == nil {
		g.names = make(map[string]*nameAdmission)
	}
	entry := g.names[name]
	if entry == nil {
		entry = &nameAdmission{available: make(chan struct{}, 1)}
		entry.available <- struct{}{}
		g.names[name] = entry
	}
	entry.refs++
	g.mu.Unlock()
	select {
	case <-closing:
		g.unref(name, entry)
		return nil, ErrNameAdmissionClosed
	case <-ctx.Done():
		g.unref(name, entry)
		return nil, ctx.Err()
	case <-entry.available:
		select {
		case <-closing:
			entry.available <- struct{}{}
			g.unref(name, entry)
			return nil, ErrNameAdmissionClosed
		default:
		}
		if err := ctx.Err(); err != nil {
			entry.available <- struct{}{}
			g.unref(name, entry)
			return nil, err
		}
		return func() { entry.available <- struct{}{}; g.unref(name, entry) }, nil
	}
}

func (g *NameGuard) unref(name string, entry *nameAdmission) {
	g.mu.Lock()
	entry.refs--
	if entry.refs == 0 {
		delete(g.names, name)
		if g.closed && len(g.names) == 0 {
			close(g.done)
		}
	}
	g.mu.Unlock()
}

// Close permanently rejects new admissions, wakes queued callers, and joins
// existing holders. Cancellation limits this wait only; call Close again to
// finish joining. It does not withdraw bindings or stop processes/lookup.
func (g *NameGuard) Close(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	if !g.closed {
		g.closed = true
		if g.closing == nil {
			g.closing = make(chan struct{})
			g.done = make(chan struct{})
		}
		close(g.closing)
		if len(g.names) == 0 {
			close(g.done)
		}
	}
	done := g.done
	g.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

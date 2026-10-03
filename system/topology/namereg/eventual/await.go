// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/pid"
)

// waiters wakes callers blocked on names that are not live yet. Waiters of one
// name share a channel that is closed when the name may have become live; a
// woken waiter re-checks the state and waits again if it is still absent.
// When nobody waits, wake costs one atomic load.
type waiters struct {
	byName map[string]*nameWait
	mu     sync.Mutex
	count  atomic.Int64
}

// nameWait is the shared channel of one name's waiters and how many hold it.
type nameWait struct {
	ch      chan struct{}
	holders int
}

// enter registers a waiter for name and returns the entry it waits on.
func (w *waiters) enter(name string) *nameWait {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.byName == nil {
		w.byName = make(map[string]*nameWait)
	}
	entry, ok := w.byName[name]
	if !ok {
		entry = &nameWait{ch: make(chan struct{})}
		w.byName[name] = entry
	}
	entry.holders++
	w.count.Add(1)
	return entry
}

// leave removes one waiter; the last holder of an entry still in the table
// removes it.
func (w *waiters) leave(name string, entry *nameWait) {
	w.mu.Lock()
	defer w.mu.Unlock()
	entry.holders--
	w.count.Add(-1)
	if entry.holders == 0 && w.byName[name] == entry {
		delete(w.byName, name)
	}
}

// wake signals the waiters of name.
func (w *waiters) wake(name string) {
	if w.count.Load() == 0 {
		return
	}
	w.mu.Lock()
	entry, ok := w.byName[name]
	if ok {
		delete(w.byName, name)
	}
	w.mu.Unlock()
	if ok {
		close(entry.ch)
	}
}

// Await returns the PID bound to name, waiting until the name is live in this
// replica or ctx ends. It never polls: it is woken by the state changes that
// can make a name live.
func (s *Service) Await(ctx context.Context, name string) (pid.PID, error) {
	for {
		entry := s.waiters.enter(name)
		if p, ok := s.state.Lookup(name); ok {
			s.waiters.leave(name, entry)
			return p, nil
		}
		select {
		case <-entry.ch:
			s.waiters.leave(name, entry)
		case <-ctx.Done():
			s.waiters.leave(name, entry)
			return pid.PID{}, ctx.Err()
		}
	}
}

// Waiting reports how many Await calls are blocked.
func (s *Service) Waiting() int { return int(s.waiters.count.Load()) }

// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	config "github.com/wippyai/runtime/api/service/cdc"
)

const (
	defaultStreamBuffer = 128
	maxStreamBuffer     = 65536
)

type subscribers struct {
	m    map[uint64]*subscription
	mu   sync.RWMutex
	next uint64
}

func newSubscribers() *subscribers {
	return &subscribers{m: make(map[uint64]*subscription)}
}

func (s *subscribers) subscribe(sourceName string, opts config.StreamOptions, wantSnapshot bool) *subscription {
	buffer := opts.Buffer
	if buffer <= 0 {
		buffer = defaultStreamBuffer
	}
	if buffer > maxStreamBuffer {
		buffer = maxStreamBuffer
	}

	s.mu.Lock()
	s.next++
	sub := &subscription{
		parent:       s,
		id:           s.next,
		sourceName:   sourceName,
		in:           make(chan config.Change, buffer),
		out:          make(chan config.Change, buffer),
		done:         make(chan struct{}),
		termCh:       make(chan struct{}),
		tables:       filterSet(opts.Tables),
		ops:          filterSet(opts.Ops),
		wantSnapshot: wantSnapshot,
	}
	if wantSnapshot {
		sub.snap = make(chan config.Change)
	}
	s.m[sub.id] = sub
	s.mu.Unlock()

	go sub.run()

	return sub
}

func (s *subscribers) publish(_ context.Context, change config.Change) {
	s.mu.RLock()
	matched := make([]*subscription, 0, len(s.m))
	for _, sub := range s.m {
		if sub.matches(change) {
			matched = append(matched, sub)
		}
	}
	s.mu.RUnlock()

	for _, sub := range matched {
		sub.send(change)
	}
}

func (s *subscribers) remove(id uint64) {
	s.mu.Lock()
	delete(s.m, id)
	s.mu.Unlock()
}

func (s *subscribers) closeAll() {
	s.mu.Lock()
	subs := make([]*subscription, 0, len(s.m))
	for id, sub := range s.m {
		subs = append(subs, sub)
		delete(s.m, id)
	}
	s.mu.Unlock()

	for _, sub := range subs {
		sub.Close()
	}
}

type subscription struct {
	parent       *subscribers
	in           chan config.Change
	out          chan config.Change
	snap         chan config.Change
	done         chan struct{}
	termCh       chan struct{}
	tables       map[string]struct{}
	ops          map[string]struct{}
	term         atomic.Pointer[config.Change]
	sourceName   string
	id           uint64
	closeOnce    sync.Once
	failOnce     sync.Once
	closed       atomic.Bool
	wantSnapshot bool
}

func (s *subscription) Changes() <-chan config.Change {
	return s.out
}

func (s *subscription) Close() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		if s.parent != nil {
			s.parent.remove(s.id)
		}
		close(s.done)
	})
}

func (s *subscription) fail(reason string) {
	s.failOnce.Do(func() {
		c := config.Change{Source: s.sourceName, Op: "error", Error: reason}
		s.term.Store(&c)
		s.closed.Store(true)
		if s.parent != nil {
			s.parent.remove(s.id)
		}
		close(s.termCh)
	})
}

func (s *subscription) run() {
	defer close(s.out)

	if s.wantSnapshot && !s.pump(s.snap, true) {
		s.flushTerm()
		return
	}
	if !s.pump(s.in, false) {
		s.flushTerm()
	}
}

func (s *subscription) pump(src <-chan config.Change, snapshotPhase bool) bool {
	for {
		select {
		case <-s.done:
			return false
		case <-s.termCh:
			return false
		case change, ok := <-src:
			if !ok {
				return snapshotPhase
			}
			select {
			case <-s.done:
				return false
			case <-s.termCh:
				return false
			case s.out <- change:
			}
		}
	}
}

func (s *subscription) flushTerm() {
	if t := s.term.Load(); t != nil {
		select {
		case s.out <- *t:
		case <-s.done:
		}
	}
}

func (s *subscription) send(change config.Change) {
	if s.closed.Load() {
		return
	}
	select {
	case s.in <- change:
	default:
		s.fail("sqlite cdc subscriber backlog overflow")
	}
}

func (s *subscription) matches(change config.Change) bool {
	if change.Op == "error" || change.Op == "snapshot" {
		return true
	}
	if len(s.ops) > 0 {
		if _, ok := s.ops[strings.ToLower(change.Op)]; !ok {
			return false
		}
	}
	if len(s.tables) > 0 {
		if _, ok := s.tables[strings.ToLower(change.Relation)]; ok {
			return true
		}
		if _, ok := s.tables[strings.ToLower(change.Table)]; ok {
			return true
		}

		return false
	}

	return true
}

func filterSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}

	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			out[v] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}

	return out
}

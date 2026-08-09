// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	config "github.com/wippyai/runtime/api/service/cdc"
)

const (
	defaultStreamBuffer = 128
	maxStreamBuffer     = 65536
)

var errSubscriberOverflow = errors.New("sqlite cdc subscriber backlog overflow")

type subscribers struct {
	m    map[uint64]*subscription
	next uint64
	mu   sync.RWMutex
}

func newSubscribers() *subscribers {
	return &subscribers{m: make(map[uint64]*subscription)}
}

func (s *subscribers) subscribe(sourceName string, opts config.StreamOptions) *subscription {
	buffer := opts.Buffer
	if buffer <= 0 {
		buffer = defaultStreamBuffer
	}
	if buffer > maxStreamBuffer {
		buffer = maxStreamBuffer
	}

	s.mu.Lock()
	s.next++
	sub := newSubscription(sourceName, opts, buffer)
	sub.parent = s
	sub.id = s.next
	s.m[sub.id] = sub
	s.mu.Unlock()
	return sub
}

func newSubscription(sourceName string, opts config.StreamOptions, buffer int) *subscription {
	return &subscription{
		sourceName: sourceName,
		changes:    make(chan config.Change, buffer),
		done:       make(chan struct{}),
		tables:     filterSet(opts.Tables),
		ops:        filterSet(opts.Ops),
	}
}

func (s *subscribers) publish(change config.Change) {
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
	s.closeWithError(nil)
}

func (s *subscribers) closeWithError(err error) {
	s.mu.Lock()
	subs := make([]*subscription, 0, len(s.m))
	for id, sub := range s.m {
		subs = append(subs, sub)
		delete(s.m, id)
	}
	s.mu.Unlock()
	for _, sub := range subs {
		sub.closeWithError(err)
	}
}

type subscription struct {
	err        error
	parent     *subscribers
	changes    chan config.Change
	done       chan struct{}
	tables     map[string]struct{}
	ops        map[string]struct{}
	sourceName string
	id         uint64
	mu         sync.Mutex
	// closedFlag lets the fan-out path reject work without taking the lock in
	// the common case. The lock is still held while sending/closing so a send
	// cannot race close(changes).
	closedFlag atomic.Bool
	closed     bool
}

func (s *subscription) Changes() <-chan config.Change { return s.changes }

func (s *subscription) Close() { s.closeWithError(nil) }

func (s *subscription) Err() error {
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	return err
}

func (s *subscription) send(change config.Change) {
	if s.closedFlag.Load() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.changes <- change:
	default:
		s.closeLocked(errSubscriberOverflow)
	}
}

func (s *subscription) closeWithError(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closeLocked(err)
	s.mu.Unlock()
	if s.parent != nil {
		s.parent.remove(s.id)
	}
}

func (s *subscription) closeLocked(err error) {
	if s.closed {
		return
	}
	s.closed = true
	s.closedFlag.Store(true)
	s.err = err
	close(s.done)
	close(s.changes)
}

func (s *subscription) matches(change config.Change) bool {
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

func (s *subscription) matchesSnapshot(change config.Change) bool {
	if len(s.tables) == 0 {
		return true
	}
	if _, ok := s.tables[strings.ToLower(change.Relation)]; ok {
		return true
	}
	_, ok := s.tables[strings.ToLower(change.Table)]
	return ok
}

func (s *subscription) isClosed() bool { return s.closedFlag.Load() }

func filterSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var _ config.Stream = (*subscription)(nil)
var _ config.ErrStream = (*subscription)(nil)

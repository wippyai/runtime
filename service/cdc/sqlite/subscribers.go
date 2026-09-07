// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"errors"
	"strings"
	"sync"

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
	sub := &subscription{
		sourceName: sourceName,
		// queue is the sole driver-owned backlog. changes is an unbuffered
		// delivery handoff, so bytes leave the budget only after a receive.
		changes:    make(chan config.Change),
		done:       make(chan struct{}),
		notify:     make(chan struct{}, 1),
		maxChanges: buffer,
		maxBytes:   opts.EffectiveMaxBytes(),
		tables:     filterSet(opts.Tables),
		ops:        filterSet(opts.Ops),
		relayDone:  make(chan struct{}),
	}
	go sub.run()
	return sub
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
	if len(matched) == 0 {
		return
	}
	// Estimate the retained size once for this source event. Fan-out must not
	// repeat a recursive walk for every subscriber.
	bytes := config.EstimateChangeBytes(change)
	for _, sub := range matched {
		sub.send(change, bytes)
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
	for _, sub := range subs {
		sub.waitRelay()
	}
}

type subscription struct {
	err         error
	ops         map[string]struct{}
	parent      *subscribers
	changes     chan config.Change
	done        chan struct{}
	notify      chan struct{}
	relayDone   chan struct{}
	tables      map[string]struct{}
	sourceName  string
	queue       []queuedChange
	maxBytes    int64
	maxChanges  int
	id          uint64
	queuedBytes int64
	mu          sync.Mutex
	closed      bool
}

type queuedChange struct {
	change config.Change
	bytes  int64
}

func (s *subscription) Changes() <-chan config.Change { return s.changes }

func (s *subscription) Close() {
	s.closeWithError(nil)
	s.waitRelay()
}

func (s *subscription) Err() error {
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	return err
}

func (s *subscription) send(change config.Change, bytes int64) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if len(s.queue) >= s.maxChanges || bytes > s.maxBytes-s.queuedBytes {
		parent, id := s.closeLocked(errSubscriberOverflow)
		s.mu.Unlock()
		if parent != nil {
			parent.remove(id)
		}
		return
	}
	s.queue = append(s.queue, queuedChange{change: change, bytes: bytes})
	s.queuedBytes += bytes
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *subscription) closeWithError(err error) {
	s.mu.Lock()
	parent, id := s.closeLocked(err)
	s.mu.Unlock()
	if parent != nil {
		parent.remove(id)
	}
}

func (s *subscription) waitRelay() {
	<-s.relayDone
}

func (s *subscription) closeLocked(err error) (*subscribers, uint64) {
	if s.closed {
		return nil, 0
	}
	s.closed = true
	s.err = err
	close(s.done)
	s.queue = nil
	s.queuedBytes = 0
	return s.parent, s.id
}

func (s *subscription) run() {
	defer close(s.relayDone)
	defer close(s.changes)
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			if s.closed {
				s.mu.Unlock()
				return
			}
			notify := s.notify
			done := s.done
			s.mu.Unlock()
			select {
			case <-notify:
			case <-done:
			}
			continue
		}
		item := s.queue[0]
		done := s.done
		s.mu.Unlock()

		select {
		case <-done:
			return
		case s.changes <- item.change:
			s.mu.Lock()
			if len(s.queue) > 0 {
				s.queuedBytes -= s.queue[0].bytes
				s.queue[0] = queuedChange{}
				if len(s.queue) == 1 {
					s.queue = s.queue[:0]
				} else {
					s.queue = s.queue[1:]
				}
			}
			s.mu.Unlock()
		}
	}
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

func (s *subscription) isClosed() bool {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	return closed
}

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

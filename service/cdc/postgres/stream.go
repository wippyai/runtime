// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"

	config "github.com/wippyai/runtime/api/service/cdc"
)

const (
	defaultStreamBuffer = 128
	maxStreamBuffer     = 65536
)

// errSubscriberOverflow is terminal for one subscription only. A consumer
// that cannot keep up must not back-pressure the replication receive loop or
// unrelated subscribers.
var errSubscriberOverflow = errors.New("postgres cdc subscriber backlog overflow")

type sourceSubscription struct {
	err         error
	tables      map[string]struct{}
	source      *Source
	done        chan struct{}
	notify      chan struct{}
	relayDone   chan struct{}
	ops         map[string]struct{}
	out         chan config.Change
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

func (s *Source) Subscribe(opts config.StreamOptions) config.Stream {
	if err := opts.Validate(); err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != sourceNew && s.state != sourceRunning {
		return nil
	}
	return s.newSubscription(opts)
}

// subscribe is the driver-facing subscription path. It holds the source
// lifecycle lock while registering the child, so Stop/fault cannot transition
// the source and close its current subscriptions between the state check and
// registration.
func (s *Source) subscribe(ctx context.Context, opts config.StreamOptions) (config.Stream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != sourceRunning || s.permanentlyClosed || s.sourceErr != nil {
		return nil, config.ErrSourceNotReady
	}
	return s.newSubscription(opts), nil
}

func (s *Source) newSubscription(opts config.StreamOptions) config.Stream {
	buffer := opts.Buffer
	if buffer <= 0 {
		buffer = defaultStreamBuffer
	}
	if buffer > maxStreamBuffer {
		buffer = maxStreamBuffer
	}

	s.subMu.Lock()
	s.nextSubID++
	sub := &sourceSubscription{
		source: s,
		id:     s.nextSubID,
		// queue is the sole driver-owned backlog. out is an unbuffered
		// delivery handoff, so bytes are released exactly after a consumer
		// receives the change rather than when it is merely enqueued.
		out:        make(chan config.Change),
		done:       make(chan struct{}),
		notify:     make(chan struct{}, 1),
		maxChanges: buffer,
		maxBytes:   opts.EffectiveMaxBytes(),
		relayDone:  make(chan struct{}),
		tables:     filterSet(opts.Tables),
		ops:        filterSet(opts.Ops),
	}
	s.subs[sub.id] = sub
	s.subMu.Unlock()

	go sub.run()
	return sub
}

func (s *Source) publishChange(ctx context.Context, change config.Change) {
	s.subMu.RLock()
	subs := make([]*sourceSubscription, 0, len(s.subs))
	for _, sub := range s.subs {
		if sub.matches(change) {
			subs = append(subs, sub)
		}
	}
	s.subMu.RUnlock()

	for _, sub := range subs {
		sub.send(ctx, change)
	}
}

func (s *Source) removeSubscription(id uint64) {
	s.subMu.Lock()
	delete(s.subs, id)
	s.subMu.Unlock()
}

func (s *Source) closeSubscriptions() {
	s.closeSubscriptionsWithError(nil)
}

func (s *Source) closeSubscriptionsWithError(err error) {
	s.subMu.Lock()
	subs := make([]*sourceSubscription, 0, len(s.subs))
	for id, sub := range s.subs {
		subs = append(subs, sub)
		delete(s.subs, id)
	}
	s.subMu.Unlock()

	for _, sub := range subs {
		sub.closeWithError(err)
	}
	for _, sub := range subs {
		sub.waitRelay()
	}
}

func (s *sourceSubscription) Changes() <-chan config.Change {
	return s.out
}

func (s *sourceSubscription) Close() {
	s.closeWithError(nil)
	s.waitRelay()
}

func (s *sourceSubscription) Err() error {
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	return err
}

func (s *sourceSubscription) closeWithError(err error) {
	s.mu.Lock()
	parent, id := s.closeLocked(err)
	s.mu.Unlock()
	if parent != nil {
		parent.removeSubscription(id)
	}
}

func (s *sourceSubscription) waitRelay() {
	<-s.relayDone
}

func (s *sourceSubscription) closeLocked(err error) (*Source, uint64) {
	if s.closed {
		return nil, 0
	}
	s.closed = true
	s.err = err
	s.queue = nil
	s.queuedBytes = 0
	close(s.done)
	return s.source, s.id
}

func (s *sourceSubscription) run() {
	defer close(s.relayDone)
	defer close(s.out)
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
		case s.out <- item.change:
			s.mu.Lock()
			if len(s.queue) > 0 {
				s.queuedBytes -= s.queue[0].bytes
				s.queue[0] = queuedChange{}
				s.queue = s.queue[1:]
			}
			s.mu.Unlock()
		}
	}
}

func (s *sourceSubscription) send(_ context.Context, change config.Change) {
	bytes := config.EstimateChangeBytes(change)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if len(s.queue) >= s.maxChanges || bytes > s.maxBytes-s.queuedBytes {
		parent, id := s.closeLocked(errSubscriberOverflow)
		s.mu.Unlock()
		if parent != nil {
			parent.removeSubscription(id)
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

func (s *sourceSubscription) matches(change config.Change) bool {
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

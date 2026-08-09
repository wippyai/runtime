// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
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

// errSubscriberOverflow is terminal for one subscription only. A consumer
// that cannot keep up must not back-pressure the replication receive loop or
// unrelated subscribers.
var errSubscriberOverflow = errors.New("postgres cdc subscriber backlog overflow")

type sourceSubscription struct {
	err    error
	source *Source
	out    chan config.Change
	done   chan struct{}
	tables map[string]struct{}
	ops    map[string]struct{}
	id     uint64
	once   sync.Once
	sendMu sync.Mutex
	closed atomic.Bool
	errMu  sync.RWMutex
}

func (s *Source) Subscribe(opts config.StreamOptions) config.Stream {
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
		// out is the only event queue. sendMu serializes producers with
		// terminal close so a slow consumer cannot retain a second buffer.
		out:    make(chan config.Change, buffer),
		done:   make(chan struct{}),
		tables: filterSet(opts.Tables),
		ops:    filterSet(opts.Ops),
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
}

func (s *sourceSubscription) Changes() <-chan config.Change {
	return s.out
}

func (s *sourceSubscription) Close() {
	s.closeWithError(nil)
}

func (s *sourceSubscription) Err() error {
	s.errMu.RLock()
	defer s.errMu.RUnlock()
	return s.err
}

func (s *sourceSubscription) closeWithError(err error) {
	s.once.Do(func() {
		// Serialize the terminal transition with send. The run goroutine closes
		// out under the same lock after done, so no producer can send to a
		// closed channel.
		s.sendMu.Lock()
		s.closed.Store(true)
		s.sendMu.Unlock()
		if err != nil {
			s.errMu.Lock()
			s.err = err
			s.errMu.Unlock()
		}
		if s.source != nil {
			s.source.removeSubscription(s.id)
		}
		close(s.done)
	})
}

func (s *sourceSubscription) run() {
	<-s.done
	s.sendMu.Lock()
	close(s.out)
	s.sendMu.Unlock()
}

func (s *sourceSubscription) send(_ context.Context, change config.Change) {
	s.sendMu.Lock()
	if s.closed.Load() {
		s.sendMu.Unlock()
		return
	}
	select {
	case s.out <- change:
		s.sendMu.Unlock()
	default:
		s.sendMu.Unlock()
		// Never wait for a slow consumer from the replication goroutine. The
		// subscription gets a terminal error and is removed; other consumers
		// continue receiving the transaction.
		s.closeWithError(errSubscriberOverflow)
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

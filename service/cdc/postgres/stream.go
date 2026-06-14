// SPDX-License-Identifier: MPL-2.0

package postgres

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

type sourceSubscription struct {
	source *Source
	in     chan config.Change
	out    chan config.Change
	done   chan struct{}
	tables map[string]struct{}
	ops    map[string]struct{}
	id     uint64
	once   sync.Once
	closed atomic.Bool
}

func (s *Source) Subscribe(opts config.StreamOptions) config.ChangeStream {
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
		in:     make(chan config.Change, buffer),
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
	s.subMu.Lock()
	subs := make([]*sourceSubscription, 0, len(s.subs))
	for id, sub := range s.subs {
		subs = append(subs, sub)
		delete(s.subs, id)
	}
	s.subMu.Unlock()

	for _, sub := range subs {
		sub.Close()
	}
}

func (s *sourceSubscription) Changes() <-chan config.Change {
	return s.out
}

func (s *sourceSubscription) Close() {
	s.once.Do(func() {
		s.closed.Store(true)
		if s.source != nil {
			s.source.removeSubscription(s.id)
		}
		close(s.done)
	})
}

func (s *sourceSubscription) run() {
	defer close(s.out)
	for {
		select {
		case <-s.done:
			return
		default:
		}
		select {
		case change := <-s.in:
			select {
			case <-s.done:
				return
			case s.out <- change:
			}
		case <-s.done:
			return
		}
	}
}

func (s *sourceSubscription) send(ctx context.Context, change config.Change) {
	if s.closed.Load() {
		return
	}
	select {
	case s.in <- change:
	case <-s.done:
	case <-ctx.Done():
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

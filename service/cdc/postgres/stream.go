// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/jackc/pglogrepl"
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
var errSnapshotNotActive = errors.New("postgres cdc snapshot is no longer active")

type sourceSubscription struct {
	err            error
	tables         map[string]struct{}
	source         *Source
	done           chan struct{}
	notify         chan struct{}
	relayDone      chan struct{}
	snapshotDone   chan struct{}
	snapshotCancel context.CancelFunc
	ops            map[string]struct{}
	out            chan config.Change
	queue          []queuedChange
	pending        []queuedChange
	maxBytes       int64
	maxChanges     int
	id             uint64
	queuedBytes    int64
	mu             sync.Mutex
	closed         bool
	snapshotting   bool
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
	if s.state != sourceNew && s.state != sourceStarting && s.state != sourceRunning {
		return nil
	}
	effectiveSnapshot := opts.Snapshot || s.snapshot
	if effectiveSnapshot && s.state != sourceRunning {
		return nil
	}
	opts.Snapshot = effectiveSnapshot
	sub := s.newSubscription(opts)
	if effectiveSnapshot {
		s.startSnapshot(context.Background(), sub)
	}
	return sub
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
	if (s.state != sourceNew && s.state != sourceStarting && s.state != sourceRunning) ||
		s.permanentlyClosed || s.sourceErr != nil {
		return nil, config.ErrSourceNotReady
	}
	effectiveSnapshot := opts.Snapshot || s.snapshot
	if effectiveSnapshot && s.state != sourceRunning {
		return nil, config.ErrSourceNotReady
	}
	opts.Snapshot = effectiveSnapshot
	sub := s.newSubscription(opts)
	if effectiveSnapshot {
		s.startSnapshot(ctx, sub)
	}
	return sub, nil
}

func (s *Source) newSubscription(opts config.StreamOptions) *sourceSubscription {
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
		out:          make(chan config.Change),
		done:         make(chan struct{}),
		notify:       make(chan struct{}, 1),
		maxChanges:   buffer,
		maxBytes:     opts.EffectiveMaxBytes(),
		snapshotting: opts.Snapshot,
		relayDone:    make(chan struct{}),
		tables:       filterSet(opts.Tables),
		ops:          filterSet(opts.Ops),
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

	if len(subs) == 0 {
		return
	}
	// Estimate the retained size once for this source event. Fan-out must not
	// repeat a recursive walk for every subscriber.
	bytes := config.EstimateChangeBytes(change)
	for _, sub := range subs {
		sub.send(ctx, change, bytes)
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
	subs := s.detachSubscriptions()
	s.closeDetachedSubscriptions(subs, err)
}

// detachSubscriptionsLocked must be called while s.mu is held. Subscribe
// takes s.mu before subMu, so this ordering makes generation cleanup atomic
// with the lifecycle transition and prevents an old run from taking a new
// generation's subscribers.
func (s *Source) detachSubscriptionsLocked() []*sourceSubscription {
	s.subMu.Lock()
	subs := make([]*sourceSubscription, 0, len(s.subs))
	for id, sub := range s.subs {
		subs = append(subs, sub)
		delete(s.subs, id)
	}
	s.subMu.Unlock()
	return subs
}

func (s *Source) detachSubscriptions() []*sourceSubscription {
	s.subMu.Lock()
	subs := make([]*sourceSubscription, 0, len(s.subs))
	for id, sub := range s.subs {
		subs = append(subs, sub)
		delete(s.subs, id)
	}
	s.subMu.Unlock()
	return subs
}

func (s *Source) closeDetachedSubscriptions(subs []*sourceSubscription, err error) {
	for _, sub := range subs {
		sub.closeWithError(err)
	}
	for _, sub := range subs {
		sub.waitSnapshot()
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
	s.waitSnapshot()
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
	cancel := s.snapshotCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if parent != nil {
		parent.removeSubscription(id)
	}
}

func (s *sourceSubscription) registerSnapshot(cancel context.CancelFunc, done chan struct{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.snapshotCancel = cancel
	s.snapshotDone = done
	return true
}

func (s *sourceSubscription) finishSnapshotWorker() {
	s.mu.Lock()
	done := s.snapshotDone
	s.snapshotDone = nil
	s.snapshotCancel = nil
	s.mu.Unlock()
	if done != nil {
		close(done)
	}
}

func (s *sourceSubscription) waitSnapshot() {
	s.mu.Lock()
	done := s.snapshotDone
	s.mu.Unlock()
	if done != nil {
		<-done
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
	s.pending = nil
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

func (s *sourceSubscription) send(_ context.Context, change config.Change, bytes int64) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if len(s.queue)+len(s.pending) >= s.maxChanges || bytes > s.maxBytes-s.queuedBytes {
		parent, id := s.closeLocked(errSubscriberOverflow)
		s.mu.Unlock()
		if parent != nil {
			parent.removeSubscription(id)
		}
		return
	}
	item := queuedChange{change: change, bytes: bytes}
	if s.snapshotting {
		s.pending = append(s.pending, item)
	} else {
		s.queue = append(s.queue, item)
	}
	s.queuedBytes += bytes
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *sourceSubscription) sendSnapshot(change config.Change, bytes int64) error {
	s.mu.Lock()
	if s.closed {
		err := s.err
		if err == nil {
			err = context.Canceled
		}
		s.mu.Unlock()
		return err
	}
	if !s.snapshotting {
		s.mu.Unlock()
		return errSnapshotNotActive
	}
	if len(s.queue)+len(s.pending) >= s.maxChanges || bytes > s.maxBytes-s.queuedBytes {
		parent, id := s.closeLocked(errSubscriberOverflow)
		s.mu.Unlock()
		if parent != nil {
			parent.removeSubscription(id)
		}
		return errSubscriberOverflow
	}
	s.queue = append(s.queue, queuedChange{change: change, bytes: bytes})
	s.queuedBytes += bytes
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
	return nil
}

func (s *sourceSubscription) finishSnapshot(fence pglogrepl.LSN, err error) {
	if err != nil {
		s.closeWithError(err)
		return
	}
	s.mu.Lock()
	if s.closed || !s.snapshotting {
		s.mu.Unlock()
		return
	}
	for _, item := range s.pending {
		if !changeAfterSnapshotFence(item.change, fence) {
			s.queuedBytes -= item.bytes
			continue
		}
		s.queue = append(s.queue, item)
	}
	s.pending = nil
	s.snapshotting = false
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func changeAfterSnapshotFence(change config.Change, fence pglogrepl.LSN) bool {
	if change.CommitLSN == "" {
		return true
	}
	commit, err := pglogrepl.ParseLSN(change.CommitLSN)
	if err != nil {
		// A malformed cursor cannot be proven to be represented by the
		// snapshot. Retain it rather than silently dropping a change.
		return true
	}
	return commit > fence
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

func (s *sourceSubscription) matchesSnapshot(change config.Change) bool {
	if len(s.tables) == 0 {
		return true
	}
	if _, ok := s.tables[strings.ToLower(change.Relation)]; ok {
		return true
	}
	_, ok := s.tables[strings.ToLower(change.Table)]
	return ok
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

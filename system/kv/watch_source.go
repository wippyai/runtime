package kv

import (
	"bytes"
	"context"
	"math"
	"strings"
	"sync"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

var (
	errWatchOverflow = kvapi.ErrWatchOverflow
	errWatchReset    = kvapi.ErrWatchReset
	errWatchClosed   = kvapi.ErrWatchClosed
	errWatchLimit    = kvapi.ErrWatchLimit
)

// watchLimits are deliberately required to be supplied by the owner of a
// source. A source does not choose policy defaults for its caller.
type watchLimits struct {
	maxWatchers int
	maxEvents   int
	maxBytes    int64
}

// watchOwner groups subscriptions which have the same lifetime. The field is
// only accessed while its source mutex is held, and makes the owner a real
// stateful token even when callers use its zero value.
type watchOwner struct {
	stopped bool
}

type watchSource struct {
	watches   map[*watchSubscription]struct{}
	closeDone chan struct{}
	mu        sync.Mutex

	limits watchLimits
	bytes  int64
	active int
	closed bool
}

type watchItem struct {
	event kvapi.WatchEvent
	bytes int64
}

// watchRecord retains existing writer-owned entries until their complete
// snapshot is published. Without subscribers, no event Entry is allocated.
type watchRecord struct {
	current  *entry
	previous *entry
	index    uint64
	revision uint64
	typ      kvapi.WatchEventType
}

type watchSubscription struct {
	err    error
	source *watchSource
	owner  *watchOwner
	prefix string

	events chan kvapi.WatchEvent
	done   chan struct{}
	worker chan struct{}
	notify chan struct{}

	queue  []watchItem
	flight watchItem

	prefixBytes int64
	head        int
	tail        int
	count       int

	hasFlight bool
	active    bool
}

func validWatchLimits(limits watchLimits) bool {
	return limits.maxWatchers > 0 && limits.maxEvents > 0 && limits.maxBytes > 0
}

func newWatchSource(limits watchLimits) (*watchSource, error) {
	if !validWatchLimits(limits) {
		return nil, errWatchLimit
	}
	return &watchSource{
		limits:    limits,
		watches:   make(map[*watchSubscription]struct{}),
		closeDone: make(chan struct{}),
	}, nil
}

func (s *watchSource) watch(ctx context.Context, prefix string, owner *watchOwner) (*watchSubscription, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, kvapi.ErrKVClosed
	}
	if owner != nil && owner.stopped {
		s.mu.Unlock()
		return nil, kvapi.ErrKVClosed
	}
	if len(s.watches) >= s.limits.maxWatchers {
		s.mu.Unlock()
		return nil, errWatchLimit
	}
	prefixBytes := int64(len(prefix))
	if prefixBytes > s.limits.maxBytes || s.bytes > s.limits.maxBytes-prefixBytes {
		s.mu.Unlock()
		return nil, errWatchLimit
	}
	sub := &watchSubscription{
		source:      s,
		owner:       owner,
		prefix:      strings.Clone(prefix),
		prefixBytes: prefixBytes,
		events:      make(chan kvapi.WatchEvent),
		done:        make(chan struct{}),
		worker:      make(chan struct{}),
		notify:      make(chan struct{}, 1),
		queue:       make([]watchItem, s.limits.maxEvents),
		active:      true,
	}
	s.watches[sub] = struct{}{}
	s.active++
	s.bytes += prefixBytes
	go sub.run(ctx)
	s.mu.Unlock()
	return sub, nil
}

// publish commits the caller's state under the same lock used for
// registration and then queues notifications. This makes the commit boundary
// atomic with respect to a new registration.
func (s *watchSource) publish(events []kvapi.WatchEvent, commit func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if commit != nil {
		commit()
	}
	if s.closed || s.active == 0 {
		return
	}
	for _, event := range events {
		s.publishEventLocked(event)
	}
}

// publishRecords converts writer-owned entries after the commit, only when
// that generation has subscribers. The gate blocks concurrent registration.
func (s *watchSource) publishRecords(records []watchRecord, commit func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if commit != nil {
		commit()
	}
	if s.closed || s.active == 0 {
		return
	}
	for _, record := range records {
		key := ""
		if record.current != nil {
			key = record.current.key
		} else if record.previous != nil {
			key = record.previous.key
		}
		observed := false
		for sub := range s.watches {
			if sub.active && strings.HasPrefix(key, sub.prefix) {
				observed = true
				break
			}
		}
		if !observed {
			continue
		}
		event := kvapi.WatchEvent{Type: record.typ, Index: record.index, Revision: record.revision}
		event.Current = entryToWatchEvent(record.current)
		event.Previous = entryToWatchEvent(record.previous)
		s.publishEventLocked(event)
	}
}

func entryToWatchEvent(e *entry) *kvapi.Entry {
	if e == nil {
		return nil
	}
	return &kvapi.Entry{Key: e.key, Value: e.value, Version: e.version, LeaseID: e.leaseID, Epoch: e.epoch}
}

func (s *watchSource) publishEventLocked(event kvapi.WatchEvent) {
	size, sizeOK := watchEventBytes(event)
	var cloned *kvapi.WatchEvent
	for sub := range s.watches {
		if !sub.active || !watchEventMatches(event, sub.prefix) {
			continue
		}
		if sub.count+boolInt(sub.hasFlight) >= s.limits.maxEvents {
			s.invalidateLocked(sub, errWatchOverflow)
			continue
		}
		if !sizeOK || size > s.limits.maxBytes || s.bytes > s.limits.maxBytes-size {
			s.invalidateLocked(sub, errWatchOverflow)
			continue
		}
		if cloned == nil {
			copy := cloneWatchEvent(event)
			cloned = &copy
		}
		s.nextItemLocked(sub, *cloned, size)
	}
}

// reset invalidates every old registration before publishing the new
// snapshot. Registration is blocked until commit returns.
func (s *watchSource) reset(commit func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.watches {
		s.invalidateLocked(sub, errWatchReset)
	}
	if commit != nil {
		commit()
	}
}

func (s *watchSource) close() {
	var workers []*watchSubscription
	s.mu.Lock()
	if s.closed {
		done := s.closeDone
		s.mu.Unlock()
		<-done
		return
	}
	s.closed = true
	for sub := range s.watches {
		if sub.active {
			s.invalidateLocked(sub, kvapi.ErrKVClosed)
		}
		workers = append(workers, sub)
	}
	s.mu.Unlock()
	for _, sub := range workers {
		<-sub.worker
	}
	close(s.closeDone)
}

func (s *watchSource) stopOwner(owner *watchOwner) {
	if owner == nil {
		return
	}
	var workers []*watchSubscription
	s.mu.Lock()
	if !owner.stopped {
		owner.stopped = true
	}
	for sub := range s.watches {
		if sub.owner == owner {
			if sub.active {
				s.invalidateLocked(sub, kvapi.ErrKVClosed)
			}
			workers = append(workers, sub)
		}
	}
	s.mu.Unlock()
	for _, sub := range workers {
		<-sub.worker
	}
}

func (s *watchSource) nextItemLocked(sub *watchSubscription, event kvapi.WatchEvent, size int64) {
	item := watchItem{event: event, bytes: size}
	sub.queue[sub.tail] = item
	sub.tail = (sub.tail + 1) % len(sub.queue)
	sub.count++
	s.bytes += size
	select {
	case sub.notify <- struct{}{}:
	default:
	}
}

func (s *watchSource) invalidate(sub *watchSubscription, reason error) {
	s.mu.Lock()
	s.invalidateLocked(sub, reason)
	s.mu.Unlock()
}

func (s *watchSource) invalidateLocked(sub *watchSubscription, reason error) {
	if !sub.active {
		return
	}
	sub.active = false
	s.active--
	sub.err = reason
	close(sub.done)
	for sub.count > 0 {
		item := &sub.queue[sub.head]
		s.bytes -= item.bytes
		*item = watchItem{}
		sub.head = (sub.head + 1) % len(sub.queue)
		sub.count--
	}
	sub.queue = nil
	sub.head = 0
	sub.tail = 0
}

func (s *watchSource) finishFlight(sub *watchSubscription) {
	s.mu.Lock()
	if sub.hasFlight {
		s.bytes -= sub.flight.bytes
		sub.flight = watchItem{}
		sub.hasFlight = false
	}
	s.mu.Unlock()
}

func (sub *watchSubscription) run(ctx context.Context) {
	defer func() {
		close(sub.events)
		sub.source.mu.Lock()
		delete(sub.source.watches, sub)
		sub.source.bytes -= sub.prefixBytes
		close(sub.worker)
		sub.source.mu.Unlock()
	}()
	for {
		item, ok := sub.take()
		if !ok {
			select {
			case <-sub.done:
				return
			case <-ctx.Done():
				sub.source.invalidate(sub, ctx.Err())
				return
			case <-sub.notify:
			}
			continue
		}

		select {
		case <-sub.done:
			sub.source.finishFlight(sub)
			return
		case <-ctx.Done():
			sub.source.invalidate(sub, ctx.Err())
			sub.source.finishFlight(sub)
			return
		default:
		}
		select {
		case <-sub.done:
			sub.source.finishFlight(sub)
			return
		case <-ctx.Done():
			sub.source.invalidate(sub, ctx.Err())
			sub.source.finishFlight(sub)
			return
		case sub.events <- item.event:
			sub.source.finishFlight(sub)
		}
	}
}

func (sub *watchSubscription) take() (watchItem, bool) {
	sub.source.mu.Lock()
	defer sub.source.mu.Unlock()
	if !sub.active || sub.count == 0 {
		return watchItem{}, false
	}
	item := sub.queue[sub.head]
	sub.queue[sub.head] = watchItem{}
	sub.head = (sub.head + 1) % len(sub.queue)
	sub.count--
	sub.flight = item
	sub.hasFlight = true
	return item, true
}

func (sub *watchSubscription) Events() <-chan kvapi.WatchEvent { return sub.events }

func (sub *watchSubscription) Done() <-chan struct{} { return sub.done }

func (sub *watchSubscription) Err() error {
	sub.source.mu.Lock()
	err := sub.err
	sub.source.mu.Unlock()
	return err
}

func (sub *watchSubscription) Close() error {
	sub.source.invalidate(sub, errWatchClosed)
	<-sub.worker
	return nil
}

func watchEventMatches(event kvapi.WatchEvent, prefix string) bool {
	if prefix == "" {
		return true
	}
	if event.Current != nil {
		return strings.HasPrefix(event.Current.Key, prefix)
	}
	return event.Previous != nil && strings.HasPrefix(event.Previous.Key, prefix)
}

func watchEventBytes(event kvapi.WatchEvent) (int64, bool) {
	var total int64
	for _, entry := range []*kvapi.Entry{event.Current, event.Previous} {
		if entry == nil {
			continue
		}
		for _, size := range []int{len(entry.Key), len(entry.LeaseID), len(entry.Value)} {
			if size < 0 || int64(size) > math.MaxInt64-total {
				return 0, false
			}
			total += int64(size)
		}
	}
	return total, true
}

func cloneWatchEvent(event kvapi.WatchEvent) kvapi.WatchEvent {
	event.Current = cloneWatchEntry(event.Current)
	event.Previous = cloneWatchEntry(event.Previous)
	return event
}

func cloneWatchEntry(entry *kvapi.Entry) *kvapi.Entry {
	if entry == nil {
		return nil
	}
	cloned := *entry
	cloned.Key = strings.Clone(entry.Key)
	cloned.LeaseID = kvapi.LeaseID(strings.Clone(string(entry.LeaseID)))
	// Values may originate in caller-owned buffers (standalone KV and CRDT).
	// One detached copy per event keeps queued changes stable; all matching
	// subscriptions share it rather than multiplying copies per consumer.
	cloned.Value = bytes.Clone(entry.Value)
	return &cloned
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

var _ interface {
	Events() <-chan kvapi.WatchEvent
	Done() <-chan struct{}
	Err() error
	Close() error
} = (*watchSubscription)(nil)

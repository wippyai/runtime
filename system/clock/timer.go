// SPDX-License-Identifier: MPL-2.0

package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
)

const timerShardCount = 64

type timerCallback func(gen uint64)

type timerEntry struct {
	timer    *time.Timer
	callback timerCallback
	firedC   chan time.Time
	// routerKey is non-nil when this timer was started by a router-
	// driven TimerStartCmd (ChID != 0). The dispatcher reads it in
	// stop handlers to clean its (pid, epoch, chID) reverse map. The
	// fire path also clears it from the dispatcher reverse map after
	// the callback runs.
	routerKey *chIDKey
	// onFireCleanup, when non-nil, runs after the callback and clears
	// the dispatcher's reverse map entry. Kept separate from callback
	// so the user-supplied payload-build closure stays decoupled from
	// reverse-map bookkeeping.
	onFireCleanup func()
	mu            sync.Mutex
	stopped       atomic.Bool
	genRef        *atomic.Uint64
	gen           uint64
}

type timerShard struct {
	timers map[uint64]*timerEntry
	mu     sync.Mutex
}

type timerRegistry struct {
	shards [timerShardCount]timerShard
	nextID atomic.Uint64
}

func newTimerRegistry() *timerRegistry {
	r := &timerRegistry{}
	for i := range r.shards {
		r.shards[i].timers = make(map[uint64]*timerEntry, 16)
	}
	return r
}

func (r *timerRegistry) getShard(id uint64) *timerShard {
	return &r.shards[id&(timerShardCount-1)]
}

func (r *timerRegistry) getEntry(id uint64) (*timerShard, *timerEntry, bool) {
	shard := r.getShard(id)
	shard.mu.Lock()
	entry, ok := shard.timers[id]
	shard.mu.Unlock()
	return shard, entry, ok
}

func (r *timerRegistry) deleteEntry(shard *timerShard, id uint64) {
	shard.mu.Lock()
	delete(shard.timers, id)
	shard.mu.Unlock()
}

func (r *timerRegistry) startWithCallback(d time.Duration, callback func()) uint64 {
	var cb timerCallback
	if callback != nil {
		cb = func(uint64) {
			callback()
		}
	}
	return r.startWithCallbackAndKey(d, cb, nil, nil, nil)
}

// startWithCallbackAndKey is the router-aware variant: the entry also
// stores a (pid, epoch, chID) reverse-map key plus an onFireCleanup
// closure invoked after the user callback runs. The dispatcher uses
// these to clean its (pid, epoch, chID) → id reverse map when the
// timer fires or is stopped.
func (r *timerRegistry) startWithCallbackAndKey(d time.Duration, callback timerCallback, routerKey *chIDKey, onFireCleanup func(), genRef *atomic.Uint64) uint64 {
	id := r.nextID.Add(1)
	shard := r.getShard(id)

	var gen uint64
	if genRef != nil {
		gen = genRef.Load()
	}
	entry := &timerEntry{
		callback:      callback,
		firedC:        make(chan time.Time, 1),
		routerKey:     routerKey,
		onFireCleanup: onFireCleanup,
		genRef:        genRef,
		gen:           gen,
	}

	entry.timer = time.AfterFunc(d, func() {
		r.fire(shard, id, entry)
	})

	shard.mu.Lock()
	shard.timers[id] = entry
	shard.mu.Unlock()

	return id
}

func (r *timerRegistry) fire(shard *timerShard, id uint64, entry *timerEntry) {
	entry.mu.Lock()
	if entry.stopped.Load() {
		entry.mu.Unlock()
		return
	}
	callback := entry.callback
	cleanup := entry.onFireCleanup
	gen := entry.gen
	if callback != nil {
		entry.callback = nil
		entry.onFireCleanup = nil
		entry.genRef = nil
		entry.timer = nil
	}
	entry.mu.Unlock()

	if callback != nil {
		r.deleteEntry(shard, id)
		entry.mu.Lock()
		entry.routerKey = nil
		entry.mu.Unlock()
		defer func() {
			_ = recover()
			if cleanup != nil {
				func() {
					defer func() { _ = recover() }()
					cleanup()
				}()
			}
		}()
		callback(gen)
	}

	select {
	case entry.firedC <- time.Now():
	default:
	}
}

// routerKey returns the (pid, epoch, chID) key for an active timer, or
// nil if it wasn't registered via the ephemeral router.
func (r *timerRegistry) routerKey(id uint64) *chIDKey {
	shard := r.getShard(id)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if entry, ok := shard.timers[id]; ok {
		entry.mu.Lock()
		defer entry.mu.Unlock()
		if entry.routerKey == nil {
			return nil
		}
		key := *entry.routerKey
		return &key
	}
	return nil
}

func (r *timerRegistry) wait(ctx context.Context, id uint64) (time.Time, error) {
	shard, entry, ok := r.getEntry(id)

	if !ok || entry.stopped.Load() {
		return time.Time{}, clockapi.ErrTimerNotFound
	}

	select {
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	case fireTime := <-entry.firedC:
		r.deleteEntry(shard, id)
		return fireTime, nil
	}
}

func (r *timerRegistry) stop(id uint64) (bool, error) {
	shard := r.getShard(id)
	shard.mu.Lock()
	entry, ok := shard.timers[id]
	if ok {
		delete(shard.timers, id)
	}
	shard.mu.Unlock()

	if !ok {
		return false, clockapi.ErrTimerNotFound
	}

	entry.stopped.Store(true)
	entry.mu.Lock()
	timer := entry.timer
	entry.callback = nil
	entry.onFireCleanup = nil
	entry.routerKey = nil
	entry.genRef = nil
	entry.timer = nil
	entry.mu.Unlock()
	stopped := false
	if timer != nil {
		stopped = timer.Stop()
	}
	return stopped, nil
}

func (r *timerRegistry) reset(id uint64, d time.Duration) (bool, error) {
	_, entry, ok := r.getEntry(id)

	if !ok {
		return false, clockapi.ErrTimerNotFound
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.stopped.Load() {
		return false, clockapi.ErrTimerNotFound
	}
	if entry.timer == nil {
		return false, clockapi.ErrTimerNotFound
	}

	if entry.genRef != nil {
		entry.gen = entry.genRef.Add(1)
	}
	wasActive := entry.timer.Reset(d)
	return wasActive, nil
}

func (r *timerRegistry) count() int {
	count := 0
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		count += len(shard.timers)
		shard.mu.Unlock()
	}
	return count
}

func (r *timerRegistry) close() {
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		for id, entry := range shard.timers {
			entry.stopped.Store(true)
			entry.mu.Lock()
			timer := entry.timer
			entry.callback = nil
			entry.onFireCleanup = nil
			entry.routerKey = nil
			entry.genRef = nil
			entry.timer = nil
			entry.mu.Unlock()
			if timer != nil {
				timer.Stop()
			}
			delete(shard.timers, id)
		}
		shard.mu.Unlock()
	}
}

package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
)

var errTimerNotFound = clockapi.ErrTimerNotFound

const timerShardCount = 64

type timerEntry struct {
	timer    *time.Timer
	callback func()
	firedC   chan time.Time
	stopped  atomic.Bool
	mu       sync.Mutex
}

type timerShard struct {
	mu     sync.Mutex
	timers map[uint64]*timerEntry
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

func (r *timerRegistry) start(d time.Duration) uint64 {
	return r.startWithCallback(d, nil)
}

func (r *timerRegistry) startWithCallback(d time.Duration, callback func()) uint64 {
	id := r.nextID.Add(1)
	shard := r.getShard(id)

	entry := &timerEntry{
		callback: callback,
		firedC:   make(chan time.Time, 1),
	}

	entry.timer = time.AfterFunc(d, func() {
		if entry.stopped.Load() {
			return
		}

		if entry.callback != nil {
			entry.callback()
		}

		select {
		case entry.firedC <- time.Now():
		default:
		}

		if entry.callback != nil {
			shard.mu.Lock()
			delete(shard.timers, id)
			shard.mu.Unlock()
		}
	})

	shard.mu.Lock()
	shard.timers[id] = entry
	shard.mu.Unlock()

	return id
}

func (r *timerRegistry) wait(ctx context.Context, id uint64) (time.Time, error) {
	shard := r.getShard(id)
	shard.mu.Lock()
	entry, ok := shard.timers[id]
	shard.mu.Unlock()

	if !ok || entry.stopped.Load() {
		return time.Time{}, errTimerNotFound
	}

	select {
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	case fireTime := <-entry.firedC:
		shard.mu.Lock()
		delete(shard.timers, id)
		shard.mu.Unlock()
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
		return false, errTimerNotFound
	}

	entry.stopped.Store(true)
	stopped := entry.timer.Stop()
	return stopped, nil
}

func (r *timerRegistry) reset(id uint64, d time.Duration) (bool, error) {
	shard := r.getShard(id)
	shard.mu.Lock()
	entry, ok := shard.timers[id]
	shard.mu.Unlock()

	if !ok {
		return false, errTimerNotFound
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.stopped.Load() {
		return false, errTimerNotFound
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
			entry.timer.Stop()
			delete(shard.timers, id)
		}
		shard.mu.Unlock()
	}
}

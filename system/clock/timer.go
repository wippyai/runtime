package clock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/resource"
)

// TimerRegistryKey is the context key for TimerRegistry.
var TimerRegistryKey = &ctxapi.Key{Name: "clock.timers", Inherit: false}

// ErrTimerNotFound is returned when timer ID doesn't exist.
var ErrTimerNotFound = errors.New("timer not found")

// ErrTimerAlreadyFired is returned when timer has already fired.
var ErrTimerAlreadyFired = errors.New("timer already fired")

const (
	timerShardCount = 64
	timerShardMask  = timerShardCount - 1
)

// timerEntry holds an active timer.
type timerEntry struct {
	timer  *time.Timer
	ch     <-chan time.Time
	fired  atomic.Bool
	closed atomic.Bool
}

// timerEntryPool reduces allocations for timer entries.
var timerEntryPool = sync.Pool{
	New: func() any { return &timerEntry{} },
}

func acquireTimerEntry() *timerEntry {
	return timerEntryPool.Get().(*timerEntry)
}

func releaseTimerEntry(e *timerEntry) {
	e.timer = nil
	e.ch = nil
	e.fired.Store(false)
	e.closed.Store(false)
	timerEntryPool.Put(e)
}

// timerShard is a single shard of the timer registry.
type timerShard struct {
	mu     sync.Mutex
	timers map[uint64]*timerEntry
}

// TimerRegistry manages active timers for a process using sharding.
type TimerRegistry struct {
	shards [timerShardCount]timerShard
	nextID atomic.Uint64
}

// NewTimerRegistry creates a new timer registry.
func NewTimerRegistry() *TimerRegistry {
	r := &TimerRegistry{}
	for i := range r.shards {
		r.shards[i].timers = make(map[uint64]*timerEntry, 16)
	}
	return r
}

func (r *TimerRegistry) getShard(id uint64) *timerShard {
	return &r.shards[id&timerShardMask]
}

// Start creates a new timer with given duration, returns its ID.
func (r *TimerRegistry) Start(d time.Duration) uint64 {
	id := r.nextID.Add(1)
	shard := r.getShard(id)

	t := time.NewTimer(d)
	entry := acquireTimerEntry()
	entry.timer = t
	entry.ch = t.C

	shard.mu.Lock()
	shard.timers[id] = entry
	shard.mu.Unlock()

	return id
}

// Wait blocks until timer fires or context is cancelled.
// Returns fire time or error if timer not found/cancelled.
func (r *TimerRegistry) Wait(ctx context.Context, id uint64) (time.Time, error) {
	shard := r.getShard(id)

	shard.mu.Lock()
	entry, ok := shard.timers[id]
	shard.mu.Unlock()

	if !ok {
		return time.Time{}, ErrTimerNotFound
	}

	if entry.fired.Load() {
		return time.Time{}, ErrTimerAlreadyFired
	}

	if entry.closed.Load() {
		return time.Time{}, ErrTimerNotFound
	}

	select {
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	case t, ok := <-entry.ch:
		if !ok {
			return time.Time{}, ErrTimerNotFound
		}
		entry.fired.Store(true)
		// Auto-cleanup after firing
		shard.mu.Lock()
		delete(shard.timers, id)
		shard.mu.Unlock()
		releaseTimerEntry(entry)
		return t, nil
	}
}

// Stop stops and removes timer with given ID.
// Returns true if timer was stopped before firing.
func (r *TimerRegistry) Stop(id uint64) (bool, error) {
	shard := r.getShard(id)

	shard.mu.Lock()
	entry, ok := shard.timers[id]
	if ok {
		delete(shard.timers, id)
	}
	shard.mu.Unlock()

	if !ok {
		return false, ErrTimerNotFound
	}

	entry.closed.Store(true)
	stopped := entry.timer.Stop()
	releaseTimerEntry(entry)
	return stopped, nil
}

// Reset resets timer with given ID to fire after new duration.
// Returns true if timer was active and reset, false if already fired.
func (r *TimerRegistry) Reset(id uint64, d time.Duration) (bool, error) {
	shard := r.getShard(id)

	shard.mu.Lock()
	entry, ok := shard.timers[id]
	shard.mu.Unlock()

	if !ok {
		return false, ErrTimerNotFound
	}

	if entry.closed.Load() {
		return false, ErrTimerNotFound
	}

	if entry.fired.Load() {
		return false, nil
	}

	return entry.timer.Reset(d), nil
}

// Close stops all timers and clears the registry.
func (r *TimerRegistry) Close() {
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		for id, entry := range shard.timers {
			entry.closed.Store(true)
			entry.timer.Stop()
			delete(shard.timers, id)
			releaseTimerEntry(entry)
		}
		shard.mu.Unlock()
	}
}

// Count returns the total number of active timers.
func (r *TimerRegistry) Count() int {
	var count int
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		count += len(shard.timers)
		shard.mu.Unlock()
	}
	return count
}

// GetTimerRegistry retrieves TimerRegistry from FrameContext.
func GetTimerRegistry(ctx context.Context) *TimerRegistry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(TimerRegistryKey); ok {
		return val.(*TimerRegistry)
	}
	return nil
}

// SetTimerRegistry stores TimerRegistry in FrameContext.
func SetTimerRegistry(ctx context.Context, r *TimerRegistry) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(TimerRegistryKey, r)
}

// GetOrCreateTimerRegistry returns existing registry or creates a new one.
func GetOrCreateTimerRegistry(ctx context.Context) *TimerRegistry {
	if r := GetTimerRegistry(ctx); r != nil {
		return r
	}
	r := NewTimerRegistry()
	SetTimerRegistry(ctx, r)

	if store := resource.GetStore(ctx); store != nil {
		store.AddCleanup(func() error {
			r.Close()
			return nil
		})
	}

	return r
}

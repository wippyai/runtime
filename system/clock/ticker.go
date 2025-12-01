package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/resource"
)

// TickerRegistryKey is the context key for TickerRegistry.
var TickerRegistryKey = &ctxapi.Key{Name: "clock.tickers", Inherit: false}

// ErrTickerNotFound is an alias for the API error.
var ErrTickerNotFound = clockapi.ErrTickerNotFound

// ErrTickerClosed is an alias for the API error.
var ErrTickerClosed = clockapi.ErrTickerClosed

const (
	tickerShardCount = 64
	tickerShardMask  = tickerShardCount - 1
)

// tickerEntry holds an active ticker and its channel.
type tickerEntry struct {
	ticker *time.Ticker
	ch     <-chan time.Time
	closed atomic.Bool
}

// tickerEntryPool reduces allocations for ticker entries.
var tickerEntryPool = sync.Pool{
	New: func() any { return &tickerEntry{} },
}

func acquireTickerEntry() *tickerEntry {
	return tickerEntryPool.Get().(*tickerEntry)
}

func releaseTickerEntry(e *tickerEntry) {
	e.ticker = nil
	e.ch = nil
	e.closed.Store(false)
	tickerEntryPool.Put(e)
}

// tickerShard is a single shard of the ticker registry.
type tickerShard struct {
	mu      sync.Mutex
	tickers map[uint64]*tickerEntry
}

// TickerRegistry manages active tickers for a process using sharding.
type TickerRegistry struct {
	shards [tickerShardCount]tickerShard
	nextID atomic.Uint64
}

// NewTickerRegistry creates a new ticker registry.
func NewTickerRegistry() *TickerRegistry {
	r := &TickerRegistry{}
	for i := range r.shards {
		r.shards[i].tickers = make(map[uint64]*tickerEntry, 16)
	}
	return r
}

func (r *TickerRegistry) getShard(id uint64) *tickerShard {
	return &r.shards[id&tickerShardMask]
}

// Start creates a new ticker with given duration, returns its ID.
func (r *TickerRegistry) Start(d time.Duration) uint64 {
	id := r.nextID.Add(1)
	shard := r.getShard(id)

	t := time.NewTicker(d)
	entry := acquireTickerEntry()
	entry.ticker = t
	entry.ch = t.C

	shard.mu.Lock()
	shard.tickers[id] = entry
	shard.mu.Unlock()

	return id
}

// Next waits for the next tick from ticker with given ID.
// Returns tick time or error if ticker not found or closed.
func (r *TickerRegistry) Next(ctx context.Context, id uint64) (time.Time, error) {
	shard := r.getShard(id)

	shard.mu.Lock()
	entry, ok := shard.tickers[id]
	shard.mu.Unlock()

	if !ok {
		return time.Time{}, ErrTickerNotFound
	}

	if entry.closed.Load() {
		return time.Time{}, ErrTickerClosed
	}

	select {
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	case t, ok := <-entry.ch:
		if !ok {
			return time.Time{}, ErrTickerClosed
		}
		return t, nil
	}
}

// Stop stops and removes ticker with given ID.
func (r *TickerRegistry) Stop(id uint64) error {
	shard := r.getShard(id)

	shard.mu.Lock()
	entry, ok := shard.tickers[id]
	if ok {
		delete(shard.tickers, id)
	}
	shard.mu.Unlock()

	if !ok {
		return ErrTickerNotFound
	}

	entry.closed.Store(true)
	entry.ticker.Stop()
	releaseTickerEntry(entry)
	return nil
}

// GetTickChan returns the tick channel for the given ticker ID.
// Used by modules that need direct access to the underlying Go channel.
func (r *TickerRegistry) GetTickChan(id uint64) (<-chan time.Time, error) {
	shard := r.getShard(id)

	shard.mu.Lock()
	entry, ok := shard.tickers[id]
	shard.mu.Unlock()

	if !ok {
		return nil, ErrTickerNotFound
	}

	if entry.closed.Load() {
		return nil, ErrTickerClosed
	}

	return entry.ch, nil
}

// Close stops all tickers and clears the registry.
func (r *TickerRegistry) Close() {
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		for id, entry := range shard.tickers {
			entry.closed.Store(true)
			entry.ticker.Stop()
			delete(shard.tickers, id)
			releaseTickerEntry(entry)
		}
		shard.mu.Unlock()
	}
}

// Count returns the total number of active tickers.
func (r *TickerRegistry) Count() int {
	var count int
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		count += len(shard.tickers)
		shard.mu.Unlock()
	}
	return count
}

// GetTickerRegistry retrieves TickerRegistry from FrameContext.
func GetTickerRegistry(ctx context.Context) *TickerRegistry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(TickerRegistryKey); ok {
		return val.(*TickerRegistry)
	}
	return nil
}

// SetTickerRegistry stores TickerRegistry in FrameContext.
func SetTickerRegistry(ctx context.Context, r *TickerRegistry) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(TickerRegistryKey, r)
}

// GetOrCreateTickerRegistry returns existing registry or creates a new one.
// Registers cleanup with resource.Store to stop all tickers on process termination.
func GetOrCreateTickerRegistry(ctx context.Context) *TickerRegistry {
	if r := GetTickerRegistry(ctx); r != nil {
		return r
	}
	r := NewTickerRegistry()
	_ = SetTickerRegistry(ctx, r)

	if store := resource.GetStore(ctx); store != nil {
		store.AddCleanup(func() error {
			r.Close()
			return nil
		})
	}

	return r
}

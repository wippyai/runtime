package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
)

// ErrTickerNotFound is an alias for the API error.
var ErrTickerNotFound = clockapi.ErrTickerNotFound

// ErrTickerClosed is an alias for the API error.
var ErrTickerClosed = clockapi.ErrTickerClosed

const (
	tickerShardCount = 64
	tickerShardMask  = tickerShardCount - 1
)

// tickerEntry holds an active ticker with its target process info.
type tickerEntry struct {
	ticker *time.Ticker
	pid    relay.PID
	topic  string
	ctx    context.Context
	cancel context.CancelFunc
	closed atomic.Bool
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

// Start creates a new ticker that sends ticks to the given process/topic via relay.
// Returns the ticker ID.
func (r *TickerRegistry) Start(ctx context.Context, d time.Duration, pid relay.PID, topic string, node relay.Node) uint64 {
	id := r.nextID.Add(1)
	shard := r.getShard(id)

	tickerCtx, cancel := context.WithCancel(ctx)

	entry := &tickerEntry{
		ticker: time.NewTicker(d),
		pid:    pid,
		topic:  topic,
		ctx:    tickerCtx,
		cancel: cancel,
	}

	shard.mu.Lock()
	shard.tickers[id] = entry
	shard.mu.Unlock()

	// Start goroutine that forwards ticks to the process via relay
	go r.forwardTicks(entry, node)

	return id
}

// forwardTicks reads from the ticker and sends ticks to the process.
func (r *TickerRegistry) forwardTicks(entry *tickerEntry, node relay.Node) {
	ticker := entry.ticker
	defer ticker.Stop()

	for {
		select {
		case <-entry.ctx.Done():
			return
		case t, ok := <-ticker.C:
			if !ok {
				return
			}

			if entry.closed.Load() {
				return
			}

			// Send tick time as nanoseconds via relay
			p := payload.NewPayload(t.UnixNano(), payload.Golang)
			pkg := relay.NewPackage(relay.PID{}, entry.pid, entry.topic, p)
			_ = node.Send(pkg)
		}
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
	entry.cancel()
	// Don't release entry here - goroutine still holds reference to fields
	return nil
}

// Close stops all tickers and clears the registry.
func (r *TickerRegistry) Close() {
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		for id, entry := range shard.tickers {
			entry.closed.Store(true)
			entry.cancel()
			delete(shard.tickers, id)
			// Don't release entry here - goroutine still holds reference
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

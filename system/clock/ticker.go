// SPDX-License-Identifier: MPL-2.0

package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

const tickerShardCount = 64

type tickerEntry struct {
	ctx    context.Context
	ticker *time.Ticker
	cancel context.CancelFunc
	pid    pid.PID
	topic  string
	closed atomic.Bool
}

type tickerShard struct {
	tickers map[uint64]*tickerEntry
	mu      sync.Mutex
}

type tickerRegistry struct {
	shards [tickerShardCount]tickerShard
	nextID atomic.Uint64
}

func newTickerRegistry() *tickerRegistry {
	r := &tickerRegistry{}
	for i := range r.shards {
		r.shards[i].tickers = make(map[uint64]*tickerEntry, 16)
	}
	return r
}

func (r *tickerRegistry) getShard(id uint64) *tickerShard {
	return &r.shards[id&(tickerShardCount-1)]
}

func (r *tickerRegistry) deleteEntry(shard *tickerShard, id uint64) (*tickerEntry, bool) {
	shard.mu.Lock()
	entry, ok := shard.tickers[id]
	if ok {
		delete(shard.tickers, id)
	}
	shard.mu.Unlock()
	return entry, ok
}

func (r *tickerRegistry) start(ctx context.Context, d time.Duration, p pid.PID, topic string, node relay.Node) uint64 {
	id := r.nextID.Add(1)
	shard := r.getShard(id)

	tickerCtx, cancel := context.WithCancel(ctx)

	entry := &tickerEntry{
		ticker: time.NewTicker(d),
		pid:    p,
		topic:  topic,
		ctx:    tickerCtx,
		cancel: cancel,
	}

	shard.mu.Lock()
	shard.tickers[id] = entry
	shard.mu.Unlock()

	go r.forwardTicks(entry, node)

	return id
}

func (r *tickerRegistry) forwardTicks(entry *tickerEntry, node relay.Node) {
	ticker := entry.ticker
	defer ticker.Stop()

	for {
		select {
		case <-entry.ctx.Done():
			return
		case t, ok := <-ticker.C:
			if !ok || entry.closed.Load() {
				return
			}
			// Prefer cancellation if tick delivery races with context cancellation.
			select {
			case <-entry.ctx.Done():
				return
			default:
			}

			sendTick(node, entry.pid, entry.topic, t)
		}
	}
}

func (r *tickerRegistry) stop(id uint64) error {
	shard := r.getShard(id)
	entry, ok := r.deleteEntry(shard, id)

	if !ok {
		return clockapi.ErrTickerNotFound
	}

	entry.closed.Store(true)
	entry.cancel()
	return nil
}

func (r *tickerRegistry) close() {
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		for id, entry := range shard.tickers {
			entry.closed.Store(true)
			entry.cancel()
			delete(shard.tickers, id)
		}
		shard.mu.Unlock()
	}
}

func (r *tickerRegistry) count() int {
	var count int
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		count += len(shard.tickers)
		shard.mu.Unlock()
	}
	return count
}

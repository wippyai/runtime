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
	// fire, when non-nil, replaces the default sendTick payload with
	// caller-supplied payload construction. Used by routed callers so each
	// fire carries a subscription frame tagged with (epoch, chID, gen).
	fire      func(at time.Time)
	routerKey *chIDKey // non-nil when registered via TickerStartCmd with ChID != 0
	topic     string
	pid       pid.PID
	closed    atomic.Bool
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
	return r.startWithFire(ctx, d, p, topic, nil, nil, node)
}

// startWithFire reserves and arms in one step. Callers that must install
// reverse-map state before the ticker can fire use reserve and arm
// separately.
func (r *tickerRegistry) startWithFire(ctx context.Context, d time.Duration, p pid.PID, topic string, fire func(at time.Time), routerKey *chIDKey, node relay.Node) uint64 {
	id := r.reserve(ctx, p, topic, fire, routerKey)
	r.arm(id, d, node)
	return id
}

// reserve allocates an id and stores the ticker entry without starting
// the time.Ticker or its forwarding goroutine. The entry is registered
// (and discoverable by stop and drain) but cannot fire until arm runs.
// routerKey, when non-nil, is removed from the dispatcher's reverseMap
// when the ticker stops. The payload construction is controlled by the
// supplied fire closure; if fire is nil the default legacy sendTick
// (int64 nanos) is used.
func (r *tickerRegistry) reserve(ctx context.Context, p pid.PID, topic string, fire func(at time.Time), routerKey *chIDKey) uint64 {
	id := r.nextID.Add(1)
	shard := r.getShard(id)

	tickerCtx, cancel := context.WithCancel(ctx)

	entry := &tickerEntry{
		pid:       p,
		topic:     topic,
		fire:      fire,
		routerKey: routerKey,
		ctx:       tickerCtx,
		cancel:    cancel,
	}

	shard.mu.Lock()
	shard.tickers[id] = entry
	shard.mu.Unlock()

	return id
}

// arm starts the time.Ticker and its forwarding goroutine for a reserved
// id. It is a no-op if the entry was stopped before arming. Arming is the
// last step so the reverse-map entry the dispatcher installs between
// reserve and arm is guaranteed present before the ticker can fire.
func (r *tickerRegistry) arm(id uint64, d time.Duration, node relay.Node) {
	shard := r.getShard(id)
	shard.mu.Lock()
	entry, ok := shard.tickers[id]
	armed := false
	if ok && !entry.closed.Load() {
		entry.ticker = time.NewTicker(d)
		armed = true
	}
	shard.mu.Unlock()

	if !armed {
		return
	}

	// forwardTicks owns the time.Ticker lifecycle from here: its deferred
	// Stop releases the ticker even if stop/close cancelled the entry
	// between the unlock above and this launch.
	go r.forwardTicks(entry, node)
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

			if entry.fire != nil {
				entry.fire(t)
			} else {
				sendTick(node, entry.pid, entry.topic, t)
			}
		}
	}
}

// routerKey returns the (pid, epoch, chID) key for an active ticker, or
// nil if it wasn't registered via the ephemeral router.
func (r *tickerRegistry) routerKey(id uint64) *chIDKey {
	shard := r.getShard(id)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if entry, ok := shard.tickers[id]; ok {
		return entry.routerKey
	}
	return nil
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

// SPDX-License-Identifier: MPL-2.0

package kvraft

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/kv"
	"github.com/wippyai/runtime/api/metrics"
)

// watchHub multiplexes Apply events to per-watcher channels with overflow
// protection. Mirrors kveventual.watchHub but emits with mode=raft.
type watchHub struct {
	collector metrics.Collector
	watchers  map[*watcher]struct{}
	space     string
	dropped   atomic.Uint64
	mu        sync.RWMutex
}

func newWatchHub(space string, coll metrics.Collector) *watchHub {
	return &watchHub{
		watchers:  make(map[*watcher]struct{}),
		space:     space,
		collector: coll,
	}
}

type watcher struct {
	ch     chan kv.Event
	prefix string
	closed sync.Once
}

const watcherBufferSize = 64

func (w *watcher) closeOnce() {
	w.closed.Do(func() { close(w.ch) })
}

// Subscribe registers a new watcher for keys matching `prefix`.
func (h *watchHub) Subscribe(ctx context.Context, prefix string) (<-chan kv.Event, func()) {
	w := &watcher{
		ch:     make(chan kv.Event, watcherBufferSize),
		prefix: prefix,
	}
	h.mu.Lock()
	h.watchers[w] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		delete(h.watchers, w)
		h.mu.Unlock()
		w.closeOnce()
	}

	go func() {
		<-ctx.Done()
		cancel()
	}()
	return w.ch, cancel
}

// Publish fans out an event to all matching watchers.
func (h *watchHub) Publish(ev kv.Event) {
	h.mu.RLock()
	matched := make([]*watcher, 0, 4)
	for w := range h.watchers {
		if strings.HasPrefix(ev.Key, w.prefix) {
			matched = append(matched, w)
		}
	}
	h.mu.RUnlock()

	for _, w := range matched {
		select {
		case w.ch <- ev:
		default:
			select {
			case <-w.ch:
				h.dropped.Add(1)
				if h.collector != nil {
					h.collector.CounterInc("kv_watch_dropped_total",
						metrics.Labels{"space": h.space, "mode": "raft"})
				}
			default:
			}
			select {
			case w.ch <- ev:
			default:
				h.dropped.Add(1)
				if h.collector != nil {
					h.collector.CounterInc("kv_watch_dropped_total",
						metrics.Labels{"space": h.space, "mode": "raft"})
				}
			}
		}
	}
}

// Close releases all watchers. Idempotent.
func (h *watchHub) Close() {
	h.mu.Lock()
	for w := range h.watchers {
		w.closeOnce()
		delete(h.watchers, w)
	}
	h.mu.Unlock()
}

// Dropped returns the lifetime overflow count.
func (h *watchHub) Dropped() uint64 { return h.dropped.Load() }

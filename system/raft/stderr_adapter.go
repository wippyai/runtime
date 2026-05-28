// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"bytes"
	"container/list"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// raftStderrAdapter is an io.Writer that captures lines from
// hashicorp/raft's TCPTransport (which writes errors directly to its
// configured Logger output) and routes them through zap with per-line
// rate limiting.
//
// Without this, broken-pipe storms during network partition (observed at
// thousands of lines/s during chaos) bypass zap entirely — no level filter,
// no sampling, no structure. The adapter:
//   - line-buffers partial writes (raft's logger writes one line at a time
//     via fmt.Fprintln but the io.Writer contract doesn't guarantee that),
//   - emits each unique message at most once per interval per first-N-bytes
//     prefix (to avoid one limiter per pid/IP/port permutation),
//   - bounds the per-prefix limiter table to maxKeys via LRU so a chaos
//     scenario that produces many distinct prefixes cannot blow heap,
//   - logs through the supplied zap.Logger at WARN.
//
// Suppressed messages are counted; when a message would have been emitted
// but is rate-limited, a periodic INFO summary at most every 10s reports the
// drop count.
type raftStderrAdapter struct {
	lastSumm time.Time
	logger   *zap.Logger
	limiters map[string]*list.Element // key -> *list.Element holding *limiterEntry
	lru      *list.List               // front = most recent, back = least recent
	buf      []byte
	mu       sync.Mutex
	maxKeys  int
	// evictCount is bumped every time the LRU has to evict the tail
	// because a new key arrived at cap. Inspected by tests.
	evictCount uint64
	// suppCount counts suppressed (rate-limited) lines across all keys.
	suppCount uint64
}

type limiterEntry struct {
	lim *rate.Limiter
	key string
}

const defaultStderrAdapterMaxKeys = 256

func newRaftStderrAdapter(logger *zap.Logger) *raftStderrAdapter {
	return newRaftStderrAdapterWithCap(logger, defaultStderrAdapterMaxKeys)
}

func newRaftStderrAdapterWithCap(logger *zap.Logger, maxKeys int) *raftStderrAdapter {
	if maxKeys <= 0 {
		maxKeys = defaultStderrAdapterMaxKeys
	}
	return &raftStderrAdapter{
		logger:   logger,
		limiters: make(map[string]*list.Element, maxKeys),
		lru:      list.New(),
		maxKeys:  maxKeys,
	}
}

func (a *raftStderrAdapter) Write(p []byte) (int, error) {
	a.mu.Lock()
	a.buf = append(a.buf, p...)
	for {
		i := bytes.IndexByte(a.buf, '\n')
		if i < 0 {
			break
		}
		line := a.buf[:i]
		a.buf = a.buf[i+1:]
		a.emitLocked(line)
	}
	a.mu.Unlock()
	return len(p), nil
}

// emitLocked emits a single trimmed line through zap, subject to rate limiting.
// Caller must hold a.mu.
func (a *raftStderrAdapter) emitLocked(line []byte) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	// Use the first 64 bytes as the limiter key. This collapses
	// "...127.0.0.1:51946: write: broken pipe" / "...:51974: ..." into
	// the same bucket, which is what we want.
	keyLen := 64
	if len(line) < keyLen {
		keyLen = len(line)
	}
	key := string(line[:keyLen])

	if elem, ok := a.limiters[key]; ok {
		// Hit: move to front of LRU.
		a.lru.MoveToFront(elem)
		entry := elem.Value.(*limiterEntry)
		if !entry.lim.Allow() {
			a.suppCount++
			a.maybeEmitSummaryLocked()
			return
		}
		a.logger.Warn(string(line))
		return
	}

	// Miss: evict LRU tail if at cap, then add.
	if a.lru.Len() >= a.maxKeys {
		oldest := a.lru.Back()
		if oldest != nil {
			oldEntry := oldest.Value.(*limiterEntry)
			delete(a.limiters, oldEntry.key)
			a.lru.Remove(oldest)
			a.evictCount++
		}
	}
	// 1 line per second steady; burst 5 to allow a small spike at
	// onset of an event without flooding.
	lim := rate.NewLimiter(rate.Every(time.Second), 5)
	entry := &limiterEntry{key: key, lim: lim}
	elem := a.lru.PushFront(entry)
	a.limiters[key] = elem

	if !lim.Allow() {
		a.suppCount++
		a.maybeEmitSummaryLocked()
		return
	}
	a.logger.Warn(string(line))
}

func (a *raftStderrAdapter) maybeEmitSummaryLocked() {
	now := time.Now()
	if now.Sub(a.lastSumm) <= 10*time.Second {
		return
	}
	a.lastSumm = now
	a.logger.Info("raft-net: suppressed repeats",
		zap.Uint64("count_total", a.suppCount),
		zap.Uint64("evicted_keys_total", a.evictCount),
		zap.Int("active_keys", a.lru.Len()))
}

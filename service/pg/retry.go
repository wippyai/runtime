// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	pgapi "github.com/wippyai/runtime/api/service/pg"
	"go.uber.org/zap"
)

// retryEntry represents a single retry attempt for a broadcast message.
type retryEntry struct {
	nextTry    time.Time
	targetNode pid.NodeID
	groups     []string
	topic      string
	pids       []pid.PID
	payloads   payload.Payloads
	id         uint64
	attempts   int
}

// retryHeap implements heap.Interface ordered by nextTry ascending.
// It is a thin alias over the retryQueue.entries slice; all heap ops go
// through this type to keep retryQueue's API focused.
type retryHeap []*retryEntry

func (h retryHeap) Len() int           { return len(h) }
func (h retryHeap) Less(i, j int) bool { return h[i].nextTry.Before(h[j].nextTry) }
func (h retryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *retryHeap) Push(x any) { *h = append(*h, x.(*retryEntry)) }

func (h *retryHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return x
}

// retryQueue manages failed broadcasts with exponential backoff. Backed
// by a min-heap on nextTry: O(log N) insert/extract instead of the
// linear scan the previous slice version did. Bounded by `cap` to keep
// memory finite under partition; on overflow the oldest (i.e. closest
// to firing) entry is dropped with a metric.
type retryQueue struct {
	service    *Service
	logger     *zap.Logger
	tel        *telemetry
	timer      *time.Timer
	notifyCh   chan struct{}
	stopCh     chan struct{}
	entries    []*retryEntry
	wg         sync.WaitGroup
	sequenceID uint64
	maxRetries int
	cap        int
	baseDelay  time.Duration
	maxDelay   time.Duration
	mu         sync.Mutex
	stopped    bool
}

const defaultRetryQueueCap = 2048

// newRetryQueue creates a new retry queue.
func newRetryQueue(service *Service, maxRetries int, baseDelay, maxDelay time.Duration, logger *zap.Logger, tel *telemetry) *retryQueue {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if baseDelay <= 0 {
		baseDelay = 100 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = time.Second
	}

	return &retryQueue{
		entries:    make([]*retryEntry, 0, 64),
		maxRetries: maxRetries,
		cap:        defaultRetryQueueCap,
		baseDelay:  baseDelay,
		maxDelay:   maxDelay,
		service:    service,
		logger:     logger,
		tel:        tel,
	}
}

// pgLabel returns the service host ID if the service is non-nil, otherwise "".
// Used so retry-queue methods stay nil-safe under unit tests.
func (rq *retryQueue) pgLabel() string {
	if rq.service == nil {
		return ""
	}
	return rq.service.hostID
}

// Start begins the retry processing loop.
func (rq *retryQueue) Start(ctx context.Context) {
	rq.mu.Lock()
	if rq.stopped {
		rq.stopped = false
	}
	rq.notifyCh = make(chan struct{}, 1)
	rq.timer = nil
	stopCh := make(chan struct{})
	rq.stopCh = stopCh
	rq.mu.Unlock()
	rq.wg.Add(1)

	go func() {
		defer rq.wg.Done()

		for {
			// Read timer channel under lock to avoid race with Stop()
			rq.mu.Lock()
			var timerCh <-chan time.Time
			if rq.timer != nil {
				timerCh = rq.timer.C
			}
			rq.mu.Unlock()

			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-rq.notifyCh:
				rq.processRetries()
			case <-timerCh:
				rq.processRetries()
			}

			// After processing, schedule next timer based on heap root.
			rq.mu.Lock()
			if len(rq.entries) > 0 {
				delay := time.Until(rq.entries[0].nextTry)
				if delay <= 0 {
					delay = time.Millisecond
				}
				if rq.timer != nil {
					rq.timer.Reset(delay)
				} else {
					rq.timer = time.NewTimer(delay)
				}
			} else if rq.timer != nil {
				rq.timer.Stop()
				rq.timer = nil
			}
			rq.mu.Unlock()
		}
	}()
}

// Stop halts the retry processing loop. Safe to call multiple times.
func (rq *retryQueue) Stop() {
	rq.mu.Lock()
	if rq.stopped {
		rq.mu.Unlock()
		return
	}
	rq.stopped = true
	if rq.timer != nil {
		rq.timer.Stop()
		rq.timer = nil
	}
	if rq.stopCh != nil {
		close(rq.stopCh)
	}
	rq.mu.Unlock()

	rq.wg.Wait()
}

// Add adds a message to the retry queue.
func (rq *retryQueue) Add(targetNode pid.NodeID, topic string, groups []string, pids []pid.PID, payloads payload.Payloads) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if rq.stopped {
		rq.logger.Debug("retry queue stopped, dropping entry",
			zap.String("node", targetNode),
			zap.String("topic", topic),
		)
		return
	}

	if len(rq.entries) >= rq.cap {
		// Drop the soonest-to-fire entry (heap root) to make room.
		// Equivalent to drop-oldest under the priority model: we keep the
		// scheduling tail (entries that have a real backoff window) over
		// near-due ones that are already failing repeatedly.
		dropped := heap.Pop((*retryHeap)(&rq.entries)).(*retryEntry)
		rq.tel.recordRetryDropped(rq.pgLabel(), dropped.topic)
		rq.logger.Debug("retry queue at cap, dropped oldest",
			zap.String("dropped_node", dropped.targetNode),
			zap.Int("cap", rq.cap),
		)
	}

	rq.sequenceID++
	entry := &retryEntry{
		id:         rq.sequenceID,
		targetNode: targetNode,
		groups:     groups,
		pids:       pids,
		topic:      topic,
		payloads:   payloads,
		attempts:   0,
		nextTry:    time.Now().Add(rq.baseDelay),
	}
	heap.Push((*retryHeap)(&rq.entries), entry)
	rq.tel.recordRetryQueueSize(rq.pgLabel(), len(rq.entries))

	rq.logger.Debug("added to retry queue",
		zap.String("node", targetNode),
		zap.Strings("groups", groups),
		zap.Uint64("id", entry.id),
	)

	// Wake the processing loop non-blocking
	select {
	case rq.notifyCh <- struct{}{}:
	default:
	}
}

// processRetries attempts to send messages that are due for retry.
func (rq *retryQueue) processRetries() {
	now := time.Now()
	var ready []*retryEntry

	rq.mu.Lock()
	for len(rq.entries) > 0 && !rq.entries[0].nextTry.After(now) {
		ready = append(ready, heap.Pop((*retryHeap)(&rq.entries)).(*retryEntry))
	}
	rq.tel.recordRetryQueueSize(rq.pgLabel(), len(rq.entries))
	rq.mu.Unlock()

	for _, entry := range ready {
		rq.attemptRetry(entry)
	}
}

// attemptRetry attempts to send a single retry message.
func (rq *retryQueue) attemptRetry(entry *retryEntry) {
	// Check circuit breaker first
	cb := rq.service.cbManager.GetCircuitBreaker(entry.targetNode)
	if !cb.Allow() {
		// Circuit is open, re-queue with backoff
		rq.requeue(entry)
		return
	}

	// Attempt the send
	var err error
	switch entry.topic {
	case pgapi.TopicJoin:
		group := ""
		if len(entry.groups) > 0 {
			group = entry.groups[0]
		}
		err = rq.service.sendJoinWithRetry(entry.targetNode, group, entry.pids)
	case pgapi.TopicLeave:
		err = rq.service.sendLeaveWithRetry(entry.targetNode, entry.pids, entry.groups)
	case pgapi.TopicDiscover:
		rq.service.sendDiscover(entry.targetNode)
		// sendDiscover handles its own circuit breaker and logging
		return
	default:
		// Unknown topic, drop
		rq.logger.Warn("unknown retry topic, dropping",
			zap.String("topic", entry.topic),
			zap.Uint64("id", entry.id),
		)
		return
	}

	if err == nil {
		// Success!
		cb.RecordSuccess()
		rq.logger.Debug("retry succeeded",
			zap.String("node", entry.targetNode),
			zap.Uint64("id", entry.id),
			zap.Int("attempts", entry.attempts+1),
		)
		return
	}

	// Failed again
	cb.RecordFailure()
	rq.requeue(entry)
}

// requeue re-queues an entry for another retry attempt with exponential backoff.
func (rq *retryQueue) requeue(entry *retryEntry) {
	entry.attempts++
	pgLabel := rq.pgLabel()
	op := entry.topic

	if entry.attempts >= rq.maxRetries {
		rq.logger.Warn("max retries exceeded, dropping message",
			zap.String("node", entry.targetNode),
			zap.Strings("groups", entry.groups),
			zap.Uint64("id", entry.id),
			zap.Int("attempts", entry.attempts),
		)
		rq.tel.recordRetryGiveup(pgLabel, op)
		return
	}

	rq.tel.recordRetry(pgLabel, op, entry.attempts)

	// baseDelay * (1<<attempts) overflows to a negative Duration once
	// attempts is large enough (maxRetries is operator-settable). Clamp on
	// both bounds: an overflowed (<=0) delay would otherwise slip past the
	// max check and schedule nextTry in the past, busy-looping the retry.
	delay := rq.baseDelay * time.Duration(1<<entry.attempts)
	if delay > rq.maxDelay || delay <= 0 {
		delay = rq.maxDelay
	}
	entry.nextTry = time.Now().Add(delay)

	rq.mu.Lock()
	if len(rq.entries) >= rq.cap {
		dropped := heap.Pop((*retryHeap)(&rq.entries)).(*retryEntry)
		rq.tel.recordRetryDropped(pgLabel, dropped.topic)
	}
	heap.Push((*retryHeap)(&rq.entries), entry)
	rq.tel.recordRetryQueueSize(pgLabel, len(rq.entries))
	rq.mu.Unlock()
}

// sendJoinWithRetry is a helper to send a join message in the batch wire
// format (joins: {group -> pid strings}). The retry queue stores entries
// per (group, pids) tuple, so a replay always carries a single-group map.
func (s *Service) sendJoinWithRetry(targetNode pid.NodeID, group string, pids []pid.PID) error {
	pidStrs := make([]string, len(pids))
	for i, p := range pids {
		pidStrs[i] = p.String()
	}

	pkg := relay.NewServicePackage(s.localNodeID, s.hostID, targetNode, s.hostID, pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  s.localNodeID,
			"joins": map[string][]string{group: pidStrs},
		}),
	)

	return s.router.Send(pkg)
}

// sendLeaveWithRetry is the leave counterpart of sendJoinWithRetry. The
// retry queue's groups slice may contain duplicates (multi-join). The
// wire format folds them into a single per-group value list with the PID
// repeated, preserving the multiplicity needed on the receiver side.
func (s *Service) sendLeaveWithRetry(targetNode pid.NodeID, pids []pid.PID, groups []string) error {
	wire := make(map[string][]string, len(groups))
	for _, g := range groups {
		for _, p := range pids {
			wire[g] = append(wire[g], p.String())
		}
	}

	pkg := relay.NewServicePackage(s.localNodeID, s.hostID, targetNode, s.hostID, pgapi.TopicLeave,
		payload.New(map[string]any{
			"from":   s.localNodeID,
			"leaves": wire,
		}),
	)

	return s.router.Send(pkg)
}

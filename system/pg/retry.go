// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
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

// retryQueue manages failed broadcasts with exponential backoff retry.
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
	baseDelay  time.Duration
	maxDelay   time.Duration
	mu         sync.Mutex
	stopped    bool
}

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
		entries:    make([]*retryEntry, 0),
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
		maxDelay:   maxDelay,
		service:    service,
		logger:     logger,
		tel:        tel,
	}
}

// Start begins the retry processing loop.
func (rq *retryQueue) Start(ctx context.Context) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if rq.stopped {
		// Reset stopped flag if restarting
		rq.stopped = false
	}

	rq.notifyCh = make(chan struct{}, 1)
	rq.timer = nil
	stopCh := make(chan struct{})
	rq.stopCh = stopCh
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

			// After processing, schedule next timer based on remaining entries
			rq.mu.Lock()
			if len(rq.entries) > 0 {
				earliest := rq.entries[0].nextTry
				for _, e := range rq.entries[1:] {
					if e.nextTry.Before(earliest) {
						earliest = e.nextTry
					}
				}
				delay := time.Until(earliest)
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

	rq.entries = append(rq.entries, entry)

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
	rq.mu.Lock()
	now := time.Now()

	// Find entries ready for retry
	var ready []*retryEntry
	var remaining []*retryEntry

	for _, entry := range rq.entries {
		if now.After(entry.nextTry) || now.Equal(entry.nextTry) {
			ready = append(ready, entry)
		} else {
			remaining = append(remaining, entry)
		}
	}

	rq.entries = remaining
	rq.mu.Unlock()

	// Process ready entries outside the lock
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

	pgLabel := rq.service.hostID
	op := entry.topic

	if entry.attempts >= rq.maxRetries {
		// Max retries exceeded, drop the message
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

	// Calculate exponential backoff
	delay := rq.baseDelay * time.Duration(1<<entry.attempts)
	if delay > rq.maxDelay {
		delay = rq.maxDelay
	}

	entry.nextTry = time.Now().Add(delay)

	rq.mu.Lock()
	rq.entries = append(rq.entries, entry)
	rq.mu.Unlock()

	rq.logger.Debug("re-queued for retry",
		zap.String("node", entry.targetNode),
		zap.Uint64("id", entry.id),
		zap.Int("attempt", entry.attempts),
		zap.Duration("delay", delay),
	)
}

// sendJoinWithRetry is a helper to send a join message.
func (s *Service) sendJoinWithRetry(targetNode pid.NodeID, group string, pids []pid.PID) error {
	pidStrs := make([]any, len(pids))
	for i, p := range pids {
		pidStrs[i] = p.String()
	}

	pkg := relay.NewServicePackage(s.localNodeID, s.hostID, targetNode, s.hostID, pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  s.localNodeID,
			"group": group,
			"pids":  pidStrs,
		}),
	)

	return s.router.Send(pkg)
}

// sendLeaveWithRetry is a helper to send a leave message.
func (s *Service) sendLeaveWithRetry(targetNode pid.NodeID, pids []pid.PID, groups []string) error {
	pidStrs := make([]any, len(pids))
	for i, p := range pids {
		pidStrs[i] = p.String()
	}

	groupStrs := make([]any, len(groups))
	for i, g := range groups {
		groupStrs[i] = g
	}

	pkg := relay.NewServicePackage(s.localNodeID, s.hostID, targetNode, s.hostID, pgapi.TopicLeave,
		payload.New(map[string]any{
			"from":   s.localNodeID,
			"pids":   pidStrs,
			"groups": groupStrs,
		}),
	)

	return s.router.Send(pkg)
}

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
	id         uint64
	targetNode pid.NodeID
	group      string
	pids       []pid.PID
	topic      string
	payloads   payload.Payloads
	attempts   int
	nextTry    time.Time
}

// retryQueue manages failed broadcasts with exponential backoff retry.
type retryQueue struct {
	entries    []*retryEntry
	sequenceID uint64
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
	service    *Service
	logger     *zap.Logger
	mu         sync.Mutex
	ticker     *time.Ticker
	stopCh     chan struct{}
	wg         sync.WaitGroup
	stopped    bool
}

// newRetryQueue creates a new retry queue.
func newRetryQueue(service *Service, maxRetries int, baseDelay, maxDelay time.Duration, logger *zap.Logger) *retryQueue {
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

	rq.ticker = time.NewTicker(50 * time.Millisecond)
	stopCh := make(chan struct{})
	rq.stopCh = stopCh
	rq.wg.Add(1)

	go func() {
		defer rq.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-rq.ticker.C:
				rq.processRetries()
			}
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
	if rq.stopCh != nil {
		close(rq.stopCh)
	}
	rq.mu.Unlock()

	if rq.ticker != nil {
		rq.ticker.Stop()
	}

	rq.wg.Wait()
}

// Add adds a message to the retry queue.
func (rq *retryQueue) Add(targetNode pid.NodeID, topic string, group string, pids []pid.PID, payloads payload.Payloads) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	rq.sequenceID++
	entry := &retryEntry{
		id:         rq.sequenceID,
		targetNode: targetNode,
		group:      group,
		pids:       pids,
		topic:      topic,
		payloads:   payloads,
		attempts:   0,
		nextTry:    time.Now().Add(rq.baseDelay),
	}

	rq.entries = append(rq.entries, entry)

	rq.logger.Debug("added to retry queue",
		zap.String("node", targetNode),
		zap.String("group", group),
		zap.Uint64("id", entry.id),
	)
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
		err = rq.service.sendJoinWithRetry(entry.targetNode, entry.group, entry.pids)
	case pgapi.TopicLeave:
		err = rq.service.sendLeaveWithRetry(entry.targetNode, entry.pids, []string{entry.group})
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

	if entry.attempts >= rq.maxRetries {
		// Max retries exceeded, drop the message
		rq.logger.Warn("max retries exceeded, dropping message",
			zap.String("node", entry.targetNode),
			zap.String("group", entry.group),
			zap.Uint64("id", entry.id),
			zap.Int("attempts", entry.attempts),
		)
		return
	}

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
	pidStrs := make([]string, len(pids))
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
	pidStrs := make([]string, len(pids))
	for i, p := range pids {
		pidStrs[i] = p.String()
	}

	pkg := relay.NewServicePackage(s.localNodeID, s.hostID, targetNode, s.hostID, pgapi.TopicLeave,
		payload.New(map[string]any{
			"from":   s.localNodeID,
			"pids":   pidStrs,
			"groups": groups,
		}),
	)

	return s.router.Send(pkg)
}

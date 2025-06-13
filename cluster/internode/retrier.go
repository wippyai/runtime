package internode

import (
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/cluster"
	"go.uber.org/zap"
)

// RetryInfo tracks retry state for a single node
type RetryInfo struct {
	retryCount   int
	nextRetry    time.Time
	currentDelay time.Duration
}

// RetryScheduler manages retry logic and exponential backoff for failed connections.
// It tracks retry state per node and determines when retry attempts should be made.
type RetryScheduler struct {
	// Retry state per node
	retryInfo map[cluster.NodeID]*RetryInfo
	retryMu   sync.RWMutex

	// Configuration
	initialDelay  time.Duration
	maxDelay      time.Duration
	checkInterval time.Duration

	logger *zap.Logger
}

// NewRetryScheduler creates a new RetryScheduler with the given configuration.
func NewRetryScheduler(config ManagerConfig, logger *zap.Logger) *RetryScheduler {
	return &RetryScheduler{
		retryInfo:     make(map[cluster.NodeID]*RetryInfo),
		initialDelay:  config.InitialRetryDelay,
		maxDelay:      config.MaxRetryDelay,
		checkInterval: config.RetryCheckInterval,
		logger:        logger.Named("retry-scheduler"),
	}
}

// ScheduleRetry schedules a retry attempt for a failed connection.
// Uses exponential backoff with jitter to prevent thundering herd.
func (rs *RetryScheduler) ScheduleRetry(nodeID cluster.NodeID) {
	rs.retryMu.Lock()
	defer rs.retryMu.Unlock()

	info, exists := rs.retryInfo[nodeID]
	if !exists {
		info = &RetryInfo{
			currentDelay: rs.initialDelay,
		}
		rs.retryInfo[nodeID] = info
	}

	// Increment retry count
	info.retryCount++

	// Calculate next retry delay with exponential backoff
	delay := rs.calculateBackoff(info.retryCount)
	info.currentDelay = delay
	info.nextRetry = time.Now().Add(delay)

	rs.logger.Debug("Scheduled retry",
		zap.String("node", string(nodeID)),
		zap.Duration("delay", delay),
		zap.Int("attempt", info.retryCount),
		zap.Time("next_retry", info.nextRetry))
}

// ResetRetry resets the retry state for a node (called on successful connection).
func (rs *RetryScheduler) ResetRetry(nodeID cluster.NodeID) {
	rs.retryMu.Lock()
	defer rs.retryMu.Unlock()

	if info, exists := rs.retryInfo[nodeID]; exists {
		if info.retryCount > 0 {
			rs.logger.Debug("Reset retry state",
				zap.String("node", string(nodeID)),
				zap.Int("previous_attempts", info.retryCount))
		}

		info.retryCount = 0
		info.currentDelay = rs.initialDelay
		info.nextRetry = time.Time{}
	}
}

// GetNodesReadyForRetry returns a list of nodes that are ready for retry attempts.
func (rs *RetryScheduler) GetNodesReadyForRetry() []cluster.NodeID {
	rs.retryMu.RLock()
	defer rs.retryMu.RUnlock()

	now := time.Now()
	var readyNodes []cluster.NodeID

	for nodeID, info := range rs.retryInfo {
		if !info.nextRetry.IsZero() && now.After(info.nextRetry) {
			readyNodes = append(readyNodes, nodeID)
		}
	}

	return readyNodes
}

// ClearRetryState removes retry state for a node (called when node leaves cluster).
func (rs *RetryScheduler) ClearRetryState(nodeID cluster.NodeID) {
	rs.retryMu.Lock()
	defer rs.retryMu.Unlock()

	if _, exists := rs.retryInfo[nodeID]; exists {
		delete(rs.retryInfo, nodeID)
		rs.logger.Debug("Cleared retry state", zap.String("node", string(nodeID)))
	}
}

// GetRetryInfo returns the current retry information for a node.
func (rs *RetryScheduler) GetRetryInfo(nodeID cluster.NodeID) (int, time.Duration, time.Time) {
	rs.retryMu.RLock()
	defer rs.retryMu.RUnlock()

	if info, exists := rs.retryInfo[nodeID]; exists {
		return info.retryCount, info.currentDelay, info.nextRetry
	}

	return 0, 0, time.Time{}
}

// GetCheckInterval returns the interval at which retry checks should be performed.
func (rs *RetryScheduler) GetCheckInterval() time.Duration {
	return rs.checkInterval
}

// calculateBackoff calculates the next retry delay using exponential backoff.
// Formula: initialDelay * 2^(attempt-1), capped at maxDelay
func (rs *RetryScheduler) calculateBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return rs.initialDelay
	}

	// Calculate: initialDelay * 2^(attempt-1)
	delay := rs.initialDelay
	for i := 1; i < attempt && delay < rs.maxDelay/2; i++ {
		delay *= 2
	}

	// Cap at maximum delay
	if delay > rs.maxDelay {
		delay = rs.maxDelay
	}

	return delay
}

// GetRetryStats returns statistics about retry attempts across all nodes.
func (rs *RetryScheduler) GetRetryStats() map[string]interface{} {
	rs.retryMu.RLock()
	defer rs.retryMu.RUnlock()

	totalNodes := len(rs.retryInfo)
	activeRetries := 0
	totalAttempts := 0

	now := time.Now()
	for _, info := range rs.retryInfo {
		totalAttempts += info.retryCount
		if !info.nextRetry.IsZero() && info.nextRetry.After(now) {
			activeRetries++
		}
	}

	return map[string]interface{}{
		"total_nodes":    totalNodes,
		"active_retries": activeRetries,
		"total_attempts": totalAttempts,
	}
}

// IsRetrying returns true if a node is currently in retry state.
func (rs *RetryScheduler) IsRetrying(nodeID cluster.NodeID) bool {
	rs.retryMu.RLock()
	defer rs.retryMu.RUnlock()

	if info, exists := rs.retryInfo[nodeID]; exists {
		return !info.nextRetry.IsZero() && time.Now().Before(info.nextRetry)
	}

	return false
}

// MarkRetryInProgress clears the nextRetry time to indicate a retry is in progress.
func (rs *RetryScheduler) MarkRetryInProgress(nodeID cluster.NodeID) {
	rs.retryMu.Lock()
	defer rs.retryMu.Unlock()

	if info, exists := rs.retryInfo[nodeID]; exists {
		info.nextRetry = time.Time{}
		rs.logger.Debug("Marked retry in progress", zap.String("node", string(nodeID)))
	}
}

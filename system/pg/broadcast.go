// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

// sendToMembers sends a message to each member PID via the router.
// It creates a defensive copy of the payloads slice for each message
// to prevent data corruption if a downstream consumer mutates the slice.
// Uses circuit breaker pattern to protect against slow nodes.
func (s *Service) sendToMembers(from pid.PID, topic string, payloads payload.Payloads, members []pid.PID) int {
	sent := 0

	// Create a timeout context if broadcastTimeout is set
	var ctx context.Context
	var cancel context.CancelFunc
	if s.broadcastTimeout > 0 {
		ctx, cancel = context.WithTimeout(s.ctx, s.broadcastTimeout)
		defer cancel()
	} else {
		ctx = s.ctx
	}

	for _, target := range members {
		// Check circuit breaker for the target node
		cb := s.cbManager.GetCircuitBreaker(target.Node)
		if !cb.Allow() {
			s.logger.Debug("circuit breaker open, skipping send",
				zap.String("target", target.String()),
			)
			continue
		}

		// Check context cancellation/timeout
		select {
		case <-ctx.Done():
			s.logger.Warn("broadcast timeout, aborting remaining sends",
				zap.Int("sent", sent),
				zap.Int("total", len(members)),
			)
			return sent
		default:
		}

		msg := relay.AcquireMessage()
		msg.Topic = topic
		// Defensive copy: each message gets its own slice so downstream
		// consumers can't corrupt other messages by mutating the slice.
		copied := make(payload.Payloads, len(payloads))
		copy(copied, payloads)
		msg.Payloads = copied
		pkg := relay.NewMessagePackage(from, target, msg)

		if err := s.router.Send(pkg); err != nil {
			s.logger.Debug("broadcast send failed",
				zap.String("target", target.String()),
				zap.Error(err),
			)
			cb.RecordFailure()
			continue
		}

		cb.RecordSuccess()
		sent++
	}

	return sent
}

// deliverMonitorEvent sends a membership event to all monitors with circuit breaker protection.
// Must be called from the event loop.
func (s *Service) deliverMonitorEventWithCircuitBreaker(group string, kind string, pids []pid.PID) {
	groupEntries := s.monitors[group]
	wildcardEntries := s.monitors[""]
	if len(groupEntries) == 0 && len(wildcardEntries) == 0 {
		return
	}

	// Build event payload matching the eventbus format
	data := map[string]any{
		"system": pgapi.EventSystem,
		"kind":   kind,
		"path":   group,
		"data": pgapi.MembershipEvent{
			Group: group,
			PIDs:  pids,
		},
	}

	deliver := func(entries []*monitorEntry) {
		for _, entry := range entries {
			// Check circuit breaker for the subscriber
			cb := s.cbManager.GetCircuitBreaker(entry.pid.Node)
			if !cb.Allow() {
				s.logger.Debug("circuit breaker open for monitor, skipping",
					zap.String("subscriber", entry.pid.String()),
				)
				continue
			}

			pkg := relay.NewPackage(pid.PID{}, entry.pid, entry.topic, payload.New(data))
			if err := s.router.Send(pkg); err != nil {
				s.logger.Debug("failed to deliver monitor event",
					zap.String("group", group),
					logPID(entry.pid),
					logError(err),
				)
				cb.RecordFailure()
			} else {
				cb.RecordSuccess()
			}
		}
	}

	deliver(groupEntries)
	deliver(wildcardEntries)
}

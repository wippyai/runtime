// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"errors"
	"time"

	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/cluster/internode"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// sendToMembers sends a message to each member PID via the router.
// The payloads slice is shared across every recipient's message:
// relay.ReleaseMessage only nils its Payloads reference (it does not
// recycle the slice or mutate the payload values), and the codec reads
// payloads read-only during encode — so there is no aliasing hazard and
// no need to copy the slice per recipient. Uses circuit breaker pattern
// to protect against slow nodes.
func (s *Service) sendToMembers(from pid.PID, topic string, payloads payload.Payloads, members []pid.PID) int {
	sent := 0

	// Create a timeout context if broadcastTimeout is set
	var ctx context.Context
	var cancel context.CancelFunc
	if s.broadcastTimeout > 0 {
		ctx, cancel = context.WithTimeout(s.currentCtx(), s.broadcastTimeout)
		defer cancel()
	} else {
		ctx = s.currentCtx()
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
		msg.Payloads = payloads
		pkg := relay.NewMessagePackage(from, target, msg)

		if err := s.router.Send(pkg); err != nil {
			if errors.Is(err, internode.ErrQueueFull) {
				// Erlang OTP `pg` semantics: fire-and-forget but observable.
				// Caller already has a sent-count; we count drops separately.
				// Don't penalize the circuit breaker — the queue being full means
				// the peer is slow, not that this call itself failed.
				s.tel.recordBroadcastDropped(s.hostID, "queue_full")
				s.logger.Debug("broadcast dropped: peer send queue full",
					zap.String("target", target.String()))
				continue
			}
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

// Broadcast sends a message to all members of a group across all nodes.
func (s *Service) Broadcast(from pid.PID, group pgapi.Group, topic string, payloads payload.Payloads) (int, error) {
	span := noopSpan
	if s.tel.tracing {
		_, span = s.tel.tracer.Start(s.currentCtx(), "pg.broadcast",
			trace.WithAttributes(
				attribute.String("pg.name", group),
				attribute.String("node.id", s.localNodeID),
			),
		)
		defer span.End()
	}

	start := time.Now()
	// Snapshot members inside the event loop for consistency.
	membersCh := make(chan []pid.PID, 1)
	if !s.submit(func() {
		membersCh <- s.state.getMembers(group)
	}) {
		err := s.submitError()
		s.tel.setSpanError(span, err)
		s.tel.recordBroadcast(group, 0, err, time.Since(start))
		return 0, err
	}

	var members []pid.PID
	select {
	case members = <-membersCh:
	case <-s.currentCtx().Done():
		s.tel.setSpanError(span, ErrServiceStopped)
		s.tel.recordBroadcast(group, 0, ErrServiceStopped, time.Since(start))
		return 0, ErrServiceStopped
	}

	// Send outside the event loop so we don't block the action queue.
	sent := s.sendToMembers(from, topic, payloads, members)
	span.SetAttributes(attribute.Int("pg.recipients", sent))
	s.tel.recordBroadcast(group, sent, nil, time.Since(start))
	s.activity.Touch()
	return sent, nil
}

// BroadcastLocal sends a message to local members of a group only.
func (s *Service) BroadcastLocal(from pid.PID, group pgapi.Group, topic string, payloads payload.Payloads) (int, error) {
	// Snapshot members inside the event loop for consistency.
	membersCh := make(chan []pid.PID, 1)
	if !s.submit(func() {
		membersCh <- s.state.getLocalMembers(group)
	}) {
		return 0, s.submitError()
	}

	var members []pid.PID
	select {
	case members = <-membersCh:
	case <-s.currentCtx().Done():
		return 0, ErrServiceStopped
	}

	// Send outside the event loop so we don't block the action queue.
	sent := s.sendToMembers(from, topic, payloads, members)
	return sent, nil
}

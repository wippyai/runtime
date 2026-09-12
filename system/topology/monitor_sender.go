// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"crypto/rand"
	"errors"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
)

// remoteWatch retains the last wire transition, including after release. All
// fields are protected by the caller shard. An uncertain operation retries the
// same reference; it cannot authorize a new successor.
type remoteWatch struct {
	binding                        relay.PeerBinding
	target                         pid.PID
	reference, previous, operation string
	active                         bool
	terminal, delivering           bool
	deliveryDone                   chan struct{}
	needsRefresh                   bool
	disconnectGeneration           uint64
	busy                           chan struct{}
}

func (t *Topology) remoteMonitor(caller, target pid.PID, want bool) error {
	t.monitorMu.Lock()
	exchange, maximum := t.monitorEndpoint, t.monitorMaxRecords
	t.monitorMu.Unlock()
	if exchange == nil {
		return errors.New("remote monitor endpoint not started")
	}
	exchange.mu.Lock()
	if exchange.stopped {
		exchange.mu.Unlock()
		return context.Canceled
	}
	exchange.calls.Add(1)
	exchange.mu.Unlock()
	defer exchange.calls.Done()
	ctx, cancel := context.WithTimeout(exchange.ctx, exchange.timeout)
	defer cancel()
	callerKey, targetKey := caller.String(), target.String()
	sh := t.getShard(callerKey)
	var lifetime *callerLifetime
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		sh.mu.Lock()
		state, exists := sh.processes[callerKey]
		if !exists || (lifetime != nil && state.remoteLifetime != lifetime) {
			sh.mu.Unlock()
			return topapi.ErrPIDNotRegistered
		}
		if !want && state.remoteWatching[targetKey] == nil {
			sh.mu.Unlock()
			return nil
		}
		if state.remoteLifetime == nil {
			binder, ok := t.router.(relay.LocalBinder)
			if !ok {
				sh.mu.Unlock()
				return relay.ErrBindingUnsupported
			}
			destination, err := binder.BindLocal(caller)
			if err != nil {
				sh.mu.Unlock()
				return err
			}
			if destination == nil {
				sh.mu.Unlock()
				return relay.ErrBindingUnsupported
			}
			lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
			state.remoteLifetime = &callerLifetime{destination: destination, ctx: lifetimeCtx, cancel: lifetimeCancel}
		}
		lifetime = state.remoteLifetime
		record := state.remoteWatching[targetKey]
		var binding relay.PeerBinding
		if resolver, ok := t.router.(relay.LocalPeerResolver); ok {
			binding, _ = resolver.LookupLocalPeer(target.Node)
		}
		if record != nil && record.binding != binding {
			sh.mu.Unlock()
			return errors.New("monitor provider registration changed")
		}
		if record == nil && want && binding != nil {
			if _, ok := binding.Receiver().(topapi.NativeMonitorProvider); !ok {
				sh.mu.Unlock()
				return errors.New("local peer does not support native monitoring")
			}
		}
		if record != nil && record.terminal {
			if !want {
				sh.mu.Unlock()
				return nil
			}
			if record.binding == nil {
				sh.mu.Unlock()
				return topapi.ErrPIDNotRegistered
			}
			// A native provider may expose a logical target (e.g. workflow ID).
			// A fresh relationship asks that provider to authorize a new view;
			// it never reuses the completed callback reference.
			record.terminal = false
			record.active = false
			record.operation = ""
		}
		if record != nil && record.delivering {
			done := record.deliveryDone
			sh.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if record != nil && record.busy != nil {
			done := record.busy
			sh.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if record == nil {
			if !want {
				sh.mu.Unlock()
				return nil
			}
			if len(state.remoteWatching) >= maximum {
				sh.mu.Unlock()
				return errRemoteMonitorCapacity
			}
			if state.remoteWatching == nil {
				state.remoteWatching = make(map[string]*remoteWatch)
			}
			record = &remoteWatch{binding: binding, target: target, reference: rand.Text(), operation: topapi.MonitorRequest}
			state.remoteWatching[targetKey] = record
		} else if record.operation == "" {
			if record.active == want && (!want || (!record.needsRefresh && record.binding == nil)) {
				sh.mu.Unlock()
				return nil
			}
			if want && record.active {
				record.operation = topapi.MonitorRequest
			} else if want {
				record.previous, record.reference = record.reference, rand.Text()
				record.operation = topapi.MonitorRequest
			} else {
				record.operation = topapi.MonitorRelease
			}
		}
		// Serialize operations for a relationship without holding a shard across IO.
		// A release after uncertain installation first reconciles that exact request;
		// a new monitor after uncertain release first reconciles that exact release.
		generation := record.disconnectGeneration
		done := make(chan struct{})
		record.busy = done
		control := remoteMonitorControl{kind: record.operation, reference: record.reference, caller: caller, target: target}
		if control.kind == topapi.MonitorRequest {
			control.previous = record.previous
		}
		sh.mu.Unlock()
		err := t.monitorRoundTrip(ctx, exchange, record, lifetime, control)
		sh.mu.Lock()
		record.busy = nil
		close(done)
		state, exists = sh.processes[callerKey]
		if !exists || state.remoteLifetime != lifetime || state.remoteWatching[targetKey] != record {
			sh.mu.Unlock()
			return topapi.ErrPIDNotRegistered
		}
		if record.terminal {
			sh.mu.Unlock()
			return nil
		}
		if err != nil {
			// Missing admission is a refusal, not a synthetic process-exit event.
			if errors.Is(err, topapi.ErrPIDNotRegistered) {
				delete(state.remoteWatching, targetKey)
				delete(state.watching, targetKey)
				if !want {
					err = nil
				}
			}
			sh.mu.Unlock()
			return err
		}
		record.needsRefresh = record.disconnectGeneration != generation
		record.active = control.kind == topapi.MonitorRequest
		record.operation = ""
		if record.active {
			if state.watching == nil {
				state.watching = make(map[string]watchedProcess)
			}
			state.watching[targetKey] = watchedProcess{PID: target, attempt: new(monitorAttempt)}
		} else {
			delete(state.watching, targetKey)
		}
		satisfied := record.active == want
		sh.mu.Unlock()
		if satisfied {
			return nil
		}
	}
}

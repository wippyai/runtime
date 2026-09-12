// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
)

// receiveMonitorCompletion owns pkg only on success. It admits the exact
// relationship before translating native control into the consumer's ordinary
// lifecycle event. A refused local delivery remains retryable, not consumed.
func (t *Topology) receiveMonitorCompletion(ctx context.Context, pkg *relay.Package, fields map[string]any) error {
	if len(fields) != 6 || fields["kind"] != topapi.Exit {
		return errors.New("invalid monitor completion")
	}
	validVersion := false
	switch version := fields["v"].(type) {
	case uint64:
		validVersion = version == remoteMonitorVersion
	case int64:
		validVersion = version == int64(remoteMonitorVersion)
	}
	ref, refOK := fields["ref"].(string)
	caller, callerOK := fields["caller"].(pid.PID)
	target, targetOK := fields["from"].(pid.PID)
	if !validVersion || !refOK || ref == "" || len(ref) > 64 || !callerOK || !targetOK || caller.Node != t.localNodeID || target.Node == "" || target.Node == t.localNodeID || target.Node != pkg.ReceivedFrom || !target.Equal(pkg.Source) || target.Host == "" || target.UniqID == "" {
		return errors.New("monitor completion identity mismatch")
	}
	err := t.deliverMonitorResult(ctx, caller, target, ref, fields["result"], pkg.ReceivedFrom, nil)
	if err == nil {
		relay.ReleasePackage(pkg)
	}
	return err
}

// deliverMonitorResult shares filtering and local delivery while keeping
// authenticated physical ingress distinct from native provider authority.
func (t *Topology) deliverMonitorResult(ctx context.Context, caller, target pid.PID, ref string, result any, ingress pid.NodeID, authorize func(*processState, *remoteWatch) bool) error {
	sh := t.getShard(caller.String())
	sh.mu.Lock()
	state, exists := sh.processes[caller.String()]
	if !exists {
		sh.mu.Unlock()
		return nil
	}
	record := state.remoteWatching[target.String()]
	if record == nil || record.reference != ref || record.terminal || (!record.active && record.operation == "") {
		sh.mu.Unlock()
		return nil
	}
	if (authorize == nil && record.binding != nil) || (authorize != nil && !authorize(state, record)) {
		sh.mu.Unlock()
		return errors.New("monitor completion registration changed")
	}
	if record.delivering {
		sh.mu.Unlock()
		return errors.New("monitor completion delivery in progress")
	}
	record.delivering = true
	record.deliveryDone = make(chan struct{})
	lifetime := state.remoteLifetime
	sh.mu.Unlock()
	// Keep control references out of the application vocabulary. Provenance stays
	// attached so existing native consumers can apply their usual source checks.
	delivery := relay.NewPackage(target, caller, topapi.TopicEvents, payload.New(map[string]any{"kind": topapi.Exit, "from": target, "result": result}))
	delivery.ReceivedFrom = ingress
	var err error
	if lifetime == nil || lifetime.destination == nil {
		err = errors.New("monitor completion requires a bound local destination")
	} else if lifetime.ctx.Err() != nil {
		err = lifetime.ctx.Err()
	} else {
		deliveryCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(lifetime.ctx, cancel)
		err = lifetime.destination.SendContext(deliveryCtx, delivery)
		stop()
		cancel()
	}
	if err != nil {
		relay.ReleasePackage(delivery)
	}
	sh.mu.Lock()
	record.delivering = false
	close(record.deliveryDone)
	record.deliveryDone = nil
	current, exists := sh.processes[caller.String()]
	if err == nil && exists && current.remoteLifetime == lifetime && current.remoteWatching[target.String()] == record && record.reference == ref {
		record.terminal = true
		record.active = false
		record.operation = ""
		delete(current.watching, target.String())
	}
	sh.mu.Unlock()
	return err
}

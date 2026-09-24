// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
)

// HandleRemotePackage applies topology requests delivered by internode before
// they reach the target process inbox. The envelope identifies both PIDs;
// decoded wire payloads are maps and retain the event kind.
func (t *Topology) HandleRemotePackage(pkg *relay.Package) (bool, error) {
	if pkg == nil || pkg.Target.Node != t.localNodeID || len(pkg.Messages) != 1 ||
		pkg.Messages[0].Topic != topology.TopicEvents || len(pkg.Messages[0].Payloads) != 1 {
		return false, nil
	}
	var kind string
	switch event := pkg.Messages[0].Payloads[0].Data().(type) {
	case map[string]any:
		kind, _ = event["kind"].(string)
	case *topology.MonitorRequestEvent:
		kind = event.Kind
	case *topology.MonitorReleaseEvent:
		kind = event.Kind
	case *topology.LinkRequestEvent:
		kind = event.Kind
	case *topology.UnlinkRequestEvent:
		kind = event.Kind
	}
	switch kind {
	case topology.MonitorRequest:
		return true, t.handleMonitorRequest(pkg.Source, pkg.Target)
	case topology.MonitorRelease:
		return true, t.handleMonitorRelease(pkg.Source, pkg.Target)
	case topology.LinkRequest:
		return true, t.handleLinkRequest(pkg.Source, pkg.Target)
	case topology.UnlinkRequest:
		return true, t.handleUnlinkRequest(pkg.Source, pkg.Target)
	default:
		return false, nil
	}
}

// handleMonitorRequest processes incoming monitor requests from remote nodes.
func (t *Topology) handleMonitorRequest(caller, target pid.PID) error {
	key := target.String()
	sh := t.getShard(key)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	state, exists := sh.processes[key]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       key,
			"operation": "monitor",
			"caller":    caller.String(),
		})
	}

	if state.watchers == nil {
		state.watchers = make(map[string]pid.PID)
	}
	state.watchers[caller.String()] = caller
	return nil
}

// handleMonitorRelease processes incoming release requests from remote nodes.
func (t *Topology) handleMonitorRelease(caller, target pid.PID) error {
	key := target.String()
	sh := t.getShard(key)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	state, exists := sh.processes[key]
	if !exists {
		return nil
	}

	delete(state.watchers, caller.String())
	return nil
}

// handleLinkRequest processes incoming link requests from remote nodes.
func (t *Topology) handleLinkRequest(from, to pid.PID) error {
	key := to.String()
	sh := t.getShard(key)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	state, exists := sh.processes[key]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       key,
			"operation": "link",
			"from":      from.String(),
		})
	}

	if state.links == nil {
		state.links = make(map[string]pid.PID)
	}
	state.links[from.String()] = from
	return nil
}

// handleUnlinkRequest processes incoming unlink requests from remote nodes.
func (t *Topology) handleUnlinkRequest(from, to pid.PID) error {
	key := to.String()
	sh := t.getShard(key)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	state, exists := sh.processes[key]
	if !exists {
		return nil
	}

	delete(state.links, from.String())
	return nil
}

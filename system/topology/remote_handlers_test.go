// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
)

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

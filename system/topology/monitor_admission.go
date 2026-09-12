// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"errors"

	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
)

// HandleRemoteMonitor is the native target-host admission seam. It borrows pkg:
// handled=true means the caller must not deliver it as an application message;
// package ownership remains with the caller on every result. The host must pass
// its configured positive bound, after its own permission/lifecycle admission.
// A nil error proves local installation only, not that a remote caller saw a reply.
func (t *Topology) HandleRemoteMonitor(pkg *relay.Package, maximum int) (handled bool, err error) {
	control, err := decodeRemoteMonitor(pkg, t.localNodeID)
	if err != nil {
		return true, err
	}
	if control == nil {
		return false, nil
	}
	if maximum <= 0 {
		return true, errors.New("remote monitoring requires a positive configured bound")
	}
	sh := t.getShard(control.target.String())
	sh.mu.Lock()
	defer sh.mu.Unlock()
	state, exists := sh.processes[control.target.String()]
	if !exists {
		return true, topapi.ErrPIDNotRegistered
	}
	if state.remoteObservers == nil {
		if control.kind == topapi.MonitorRelease {
			return true, errRemoteMonitorConflict
		}
		state.remoteObservers, err = newRemoteMonitorSet(maximum)
		if err != nil {
			return true, err
		}
	}
	// The bound belongs to the lifetime. A later config change must not silently
	// expand or reinterpret an existing observer set.
	if state.remoteObservers.maximum != maximum {
		return true, errors.New("remote monitor bound changed during target lifetime")
	}
	if control.kind == topapi.MonitorRelease {
		return true, state.remoteObservers.release(control.caller, control.reference)
	}
	return true, state.remoteObservers.establish(control.caller, control.previous, control.reference)
}

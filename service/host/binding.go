// SPDX-License-Identifier: MPL-2.0
package host

import (
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// BindLocal exposes generation-qualified actor admission through its owning
// host. It cannot bind runtime control hosts or another host's process.
func (h *Host) BindLocal(target pid.PID) (relay.ContextSender, error) {
	if target.Host != h.id.String() {
		return nil, relay.ErrBindingTarget
	}
	if h.shutdown.Load() {
		return nil, ErrHostShuttingDown
	}
	if !h.running.Load() {
		return nil, ErrHostNotRunning
	}
	return h.scheduler.BindLocal(target)
}

var _ relay.LocalBinder = (*Host)(nil)

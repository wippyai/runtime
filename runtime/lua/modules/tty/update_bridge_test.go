// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"sync/atomic"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type updateReceiver struct{ terminal atomic.Bool }

func (r *updateReceiver) Send(pkg *relay.Package) error {
	if len(pkg.Messages) == 1 {
		values := pkg.Messages[0].Payloads
		if len(values) > 0 && payload.IsTerminal(values[len(values)-1]) {
			r.terminal.Store(true)
		}
	}
	relay.ReleasePackage(pkg)
	return nil
}

func TestUpdateBridgeClosesSchedulerStreamWhenViewportDetaches(t *testing.T) {
	updates := make(chan ttyapi.Update)
	close(updates)
	receiver := &updateReceiver{}
	bridge := &updateBridge{done: make(chan struct{})}
	generation := &atomic.Uint64{}

	bridge.run(updates, receiver, pid.PID{Host: "test", UniqID: "1"}, "updates", 1, generation, make(chan struct{}))
	if !receiver.terminal.Load() {
		t.Fatal("closed viewport did not terminate its scheduler stream")
	}
}

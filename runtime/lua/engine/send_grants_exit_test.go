// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/topology"
)

// An exit event reports a process that can never be addressed again; a
// restricted process drops its grant to it.
func TestExitEventRevokesSendGrant(t *testing.T) {
	exited := pid.PID{Host: "app:host", UniqID: "exited"}
	alive := pid.PID{Host: "app:host", UniqID: "alive"}
	grants := secapi.NewProcessSendGrants(exited, alive)

	proc := mustNewProcess(t, WithScript("return 1", "test.lua"))
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	if err := fc.SetMultiple(secapi.ProcessSendGrantsPair(grants)); err != nil {
		t.Fatal(err)
	}
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	proc.SetTrapLinks(true)

	for _, kind := range []topology.Kind{topology.Exit, topology.LinkDown} {
		grants.Grant(exited)
		proc.messageQueue = append(proc.messageQueue, queuedMessage{
			Topic:    topology.TopicEvents,
			Source:   exited,
			Payloads: []payload.Payload{payload.New(&topology.ExitEvent{From: exited, Kind: kind})},
		})
		proc.flushMessageQueue(proc.subs)
		if grants.Holds(exited) {
			t.Errorf("%s event leaves the grant to the exited process", kind)
		}
	}
	if !grants.Holds(alive) {
		t.Error("an exit event leaves other grants alone")
	}
}

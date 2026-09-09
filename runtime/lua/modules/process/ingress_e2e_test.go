// SPDX-License-Identifier: MPL-2.0
package process_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

func TestScheduledLuaInboxPreservesIngressAndClearsLocalProvenance(t *testing.T) {
	tc := setupProcessLeakTest(t, 2)
	defer tc.Close(t)
	frameCtx, runPID := tc.frameCtxPID(t)
	proc := newProcessLeakProcess(t, `
  local inbox = process.inbox()
  local first = inbox:receive()
  local old = first:ingress()
  assert(old ~= nil and old:node() == "peer")
  assert(old:authenticated() and old:integrity_protected())
  assert(not old:live())
  local second = inbox:receive()
  local current = second:ingress()
  assert(current ~= nil and current:live())
  assert(not old:same_connection(current))
  local third = inbox:receive()
  assert(third:ingress() == nil)
  return true
 `)
	ctx, cancel := context.WithTimeout(frameCtx, 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		value, err := tc.scheduler.Execute(ctx, runPID, proc, "", nil)
		if err == nil && value != nil {
			err = value.Error
		}
		result <- err
	}()
	require.Eventually(t, func() bool {
		for _, info := range tc.scheduler.ListProcesses() {
			if info.State == "idle" || info.State == "blocked" {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	closed, live := make(chan struct{}), make(chan struct{})
	close(closed)
	for _, connection := range []<-chan struct{}{closed, live, nil} {
		pkg := relay.NewPackage(pid.PID{Node: "claimed-origin"}, runPID, "request", payload.NewString("hello"))
		if connection != nil {
			pkg.Ingress = relay.IngressIdentity{Node: "peer", Authenticated: true, IntegrityProtected: true, ConnectionClosed: connection}
		}
		require.NoError(t, tc.scheduler.Send(pkg))
	}
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("Lua inbox did not complete:", ctx.Err())
	}
}

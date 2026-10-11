// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/runtime/resource"
	secapi "github.com/wippyai/runtime/api/security"
	terminalapi "github.com/wippyai/runtime/api/service/terminal"
	securitysys "github.com/wippyai/runtime/system/security"
)

func TestHost_PrepareContextDoesNotRetainCallerResources(t *testing.T) {
	hostCtx, cancelHost := context.WithCancel(context.Background())
	defer cancelHost()
	h := &Host{ctx: hostCtx, raw: NewRawManager(nil)}
	callerCtx, cancelCaller := context.WithCancel(context.Background())
	defer cancelCaller()
	completed := false
	callerCtx, callerFrame := ctxapi.ForkFrameContextWithCompletion(callerCtx, func() { completed = true })
	defer ctxapi.ReleaseFrameContext(callerFrame)
	callerStore := resource.NewStore()
	callerClosed := false
	callerStore.AddCleanup(func() error { callerClosed = true; return nil })
	require.NoError(t, resource.SetStore(callerCtx, callerStore))
	actor := secapi.Actor{ID: "caller"}
	scope := securitysys.NewScope(nil)
	values := ctxapi.NewValues()
	values.Set("origin", "caller")
	require.NoError(t, callerFrame.SetMultiple(secapi.ActorPair(actor), secapi.ScopePair(scope), ctxapi.ValuesPair(values)))

	processID := pid.PID{Host: "terminal", UniqID: "independent"}
	start := &process.Start{Source: registry.NewID("test", "terminal"), Input: payload.Payloads{payload.New("arg")}}
	actorCtx := h.prepareContext(callerCtx, processID, start)
	actorFrame := ctxapi.FrameFromContext(actorCtx)
	require.NotNil(t, actorFrame)
	defer ctxapi.ReleaseFrameContext(actorFrame)
	require.Nil(t, resource.GetStore(actorCtx))
	actorStore := resource.NewStore()
	require.NoError(t, resource.SetStore(actorCtx, actorStore))
	require.NotSame(t, callerStore, actorStore)
	actorClosed := false
	actorStore.AddCleanup(func() error { actorClosed = true; return nil })

	cancelCaller()
	ctxapi.ReleaseFrameContext(callerFrame)
	require.True(t, callerClosed, "a terminal actor must not retain the caller's resource lifetime")
	require.True(t, completed, "caller completion must not wait for an independent terminal actor")
	require.False(t, actorClosed)
	require.NoError(t, actorCtx.Err())
	gotActor, ok := secapi.GetActor(actorCtx)
	require.True(t, ok)
	require.Equal(t, actor, gotActor)
	gotScope, ok := secapi.GetScope(actorCtx)
	require.True(t, ok)
	require.Same(t, scope, gotScope)
	actorValues := ctxapi.GetValues(actorCtx)
	require.NotNil(t, actorValues)
	actorValues.Set("origin", "actor")
	origin, _ := values.Get("origin")
	require.Equal(t, "caller", origin)
	tc := terminalapi.GetTerminalContext(actorCtx)
	require.NotNil(t, tc)
	require.Equal(t, []string{"arg"}, tc.Args)
	require.NotNil(t, tc.Input)
	gotPID, ok := runtime.GetFramePID(actorCtx)
	require.True(t, ok)
	require.Equal(t, processID, gotPID)

	cancelHost()
	require.ErrorIs(t, actorCtx.Err(), context.Canceled)
	ctxapi.ReleaseFrameContext(actorFrame)
	require.True(t, actorClosed)
}

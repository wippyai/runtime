// SPDX-License-Identifier: MPL-2.0

package host

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/runtime/resource"
	secapi "github.com/wippyai/runtime/api/security"
	securitysys "github.com/wippyai/runtime/system/security"
)

func TestHost_PrepareContextDoesNotRetainCallerResources(t *testing.T) {
	for _, mode := range []string{"inherited", "explicit_override"} {
		t.Run(mode, func(t *testing.T) {
			type hostContextKey struct{}
			hostCtx, cancelHost := context.WithCancel(context.WithValue(context.Background(), hostContextKey{}, "host"))
			defer cancelHost()
			h := &Host{ctx: hostCtx}
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
			privateKey := &ctxapi.Key{Name: "caller.private"}
			require.NoError(t, callerFrame.Set(privateKey, "not inherited"))

			processID := pid.PID{Host: "test", UniqID: "independent"}
			start := &process.Start{Source: registry.NewID("test", "actor"), Options: namedOptions("independent")}
			if mode == "explicit_override" {
				actor = secapi.Actor{ID: "explicit"}
				scope = securitysys.NewScope(nil)
				start.Context = []ctxapi.Pair{secapi.ActorPair(actor), secapi.ScopePair(scope)}
			}
			actorCtx := h.prepareContext(callerCtx, processID, start)
			actorFrame := ctxapi.FrameFromContext(actorCtx)
			require.NotNil(t, actorFrame)
			defer ctxapi.ReleaseFrameContext(actorFrame)
			require.Nil(t, resource.GetStore(actorCtx), "a spawned actor must not inherit its caller's resource store")
			require.False(t, actorFrame.Has(privateKey))
			actorStore := resource.NewStore()
			require.NoError(t, resource.SetStore(actorCtx, actorStore))
			require.NotSame(t, callerStore, actorStore)
			actorClosed := false
			actorStore.AddCleanup(func() error { actorClosed = true; return nil })

			cancelCaller()
			ctxapi.ReleaseFrameContext(callerFrame)
			require.True(t, callerClosed, "an independent actor must not retain the caller's resource lifetime")
			require.True(t, completed, "caller completion must not wait for an independent actor")
			require.False(t, actorClosed)
			require.NoError(t, actorCtx.Err(), "actor cancellation comes from the host, not its caller")
			require.Equal(t, "host", actorCtx.Value(hostContextKey{}))
			gotActor, ok := secapi.GetActor(actorCtx)
			require.True(t, ok)
			require.Equal(t, actor, gotActor)
			gotScope, ok := secapi.GetScope(actorCtx)
			require.True(t, ok)
			require.Same(t, scope, gotScope)
			actorValues := ctxapi.GetValues(actorCtx)
			require.NotNil(t, actorValues)
			origin, ok := actorValues.Get("origin")
			require.True(t, ok)
			require.Equal(t, "caller", origin)
			actorValues.Set("origin", "actor")
			origin, _ = values.Get("origin")
			require.Equal(t, "caller", origin, "inherited mutable values must still be cloned")
			gotID, ok := runtime.GetFrameID(actorCtx)
			require.True(t, ok)
			require.Equal(t, start.Source, gotID)
			gotPID, ok := runtime.GetFramePID(actorCtx)
			require.True(t, ok)
			require.Equal(t, processID, gotPID)

			cancelHost()
			require.ErrorIs(t, actorCtx.Err(), context.Canceled)
			ctxapi.ReleaseFrameContext(actorFrame)
			require.True(t, actorClosed)
		})
	}
}

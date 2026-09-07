// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
	relaysys "github.com/wippyai/runtime/system/relay"
	ttysys "github.com/wippyai/runtime/system/tty"
)

// The Lua materialization path is shared by local and remote cached snapshots.
// Run the loop in Lua so per-iteration Go-to-Lua call overhead is not included.
func BenchmarkLuaViewportSnapshot(b *testing.B) {
	for _, mode := range []string{"full_frame", "unchanged_revision"} {
		b.Run(mode, func(b *testing.B) {
			service := ttysys.NewService()
			defer service.Close()
			ctx, frame := ctxapi.OpenFrameContext(ttyapi.WithService(ctxapi.NewRootContext(), service))
			defer frame.Close()
			ctx = relay.WithNode(ctx, relaysys.NewNode("n"))
			require.NoError(b, runtime.SetFramePID(ctx, pid.PID{Node: "n", Host: "h", UniqID: "owner"}))
			view, err := service.Create(ctx, 120, 40)
			require.NoError(b, err)
			binding, err := service.Binding(view.Grant())
			require.NoError(b, err)
			port, err := binding.Resolve(ctx)
			require.NoError(b, err)
			surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
			require.NoError(b, err)
			rows := make([]string, 40)
			for i := range rows {
				rows[i] = strings.Repeat("x", 120)
			}
			_, err = surface.Present(ttyapi.Frame{Rows: rows})
			require.NoError(b, err)
			l := lua.NewState()
			defer l.Close()
			bindTTY(l)
			l.SetContext(ctx)
			pushViewport(l, view)
			l.Pop(1) // nil error result
			l.SetGlobal("view", l.Get(-1))
			l.Pop(1)
			l.SetGlobal("revision", lua.LInteger(view.Snapshot().Revision))
			body := "view:snapshot()"
			if mode == "unchanged_revision" {
				body = "view:snapshot(revision)"
			}
			require.NoError(b, l.DoString("function sample(n) for i=1,n do "+body+" end end"))
			fn := l.GetGlobal("sample")
			b.ReportAllocs()
			b.ResetTimer()
			err = l.CallByParam(lua.P{Fn: fn, NRet: 0, Protect: true}, lua.LInteger(b.N))
			b.StopTimer()
			require.NoError(b, err)
		})
	}
}

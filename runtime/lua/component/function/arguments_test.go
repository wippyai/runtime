// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	"go.uber.org/zap"
)

func TestDeclaredArgumentsAcrossPoolsAndReload(t *testing.T) {
	for _, poolType := range []string{api.PoolTypeInline, api.PoolTypeLazy, api.PoolTypeStatic, api.PoolTypeAdaptive} {
		t.Run(poolType, func(t *testing.T) {
			cm, err := code.NewCodeManager(zap.NewNop(), nil, code.Config{})
			require.NoError(t, err)
			id := registry.NewID("test", "arguments")
			node := code.Node{ID: id, Kind: api.Function, Source: `return {run=function(id: string) return id end}`, Method: "run"}
			require.NoError(t, cm.AddNode(context.Background(), node, nil))
			m := NewManager(zap.NewNop(), cm, nil, nil, nil, engine.NewProcessFactory(cm))
			cfg := &configEntry{method: "run", pool: api.PoolConfig{Type: poolType, Workers: 1, MaxSize: 1}}
			old, err := m.buildPool(id, cfg)
			require.NoError(t, err)
			old.Start()
			defer old.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			call := func(p funcpool.Pool, arg lua.LValue, invalid bool) {
				t.Helper()
				callCtx, frame := ctxapi.OpenFrameContext(ctx)
				defer ctxapi.ReleaseFrameContext(frame)
				require.NoError(t, runtime.SetFramePID(callCtx, (&pid.PID{Host: id.String(), UniqID: "arguments"}).Precomputed()))
				result, err := p.Call(callCtx, "run", payload.Payloads{payload.NewPayload(arg, payload.Lua)})
				require.NoError(t, err)
				require.NotNil(t, result)
				if invalid {
					var apiErr apierror.Error
					require.ErrorAs(t, result.Error, &apiErr)
					require.Equal(t, apierror.Invalid, apiErr.Kind())
				} else {
					require.NoError(t, result.Error)
				}
			}
			call(old, lua.LInteger(42), true)
			call(old, lua.LString("ok"), false)
			node.Source = `return {run=function(id: integer) return id end}`
			require.NoError(t, cm.UpdateNode(context.Background(), node, nil))
			fresh, err := m.buildPool(id, cfg)
			require.NoError(t, err)
			fresh.Start()
			defer fresh.Stop()
			call(fresh, lua.LString("wrong"), true)
			call(fresh, lua.LInteger(42), false)
			call(old, lua.LString("still old"), false)
			call(old, lua.LInteger(42), true)
		})
	}
}

// SPDX-License-Identifier: MPL-2.0

package code

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"go.uber.org/zap"
)

func TestArgumentContractsSurviveCacheRestart(t *testing.T) {
	for _, compileCache := range []bool{false, true} {
		t.Run(map[bool]string{false: "typecheck", true: "compiled"}[compileCache], func(t *testing.T) {
			cfg := Config{Cache: cache.Config{Enabled: true, CompileEnabled: compileCache, TypecheckEnabled: true, Dir: t.TempDir(), ToolchainIdentity: "arguments-test"}}
			id := registry.NewID("test", "arguments")
			for range 2 {
				cm, err := NewCodeManager(zap.NewNop(), nil, cfg)
				require.NoError(t, err)
				// A fresh manager has no retained proto or in-memory manifest.
				require.NoError(t, cm.AddNode(context.Background(), Node{ID: id, Kind: api.Function, Source: `type User = {name: string}; return {run=function(user: User) return user end}`, Method: "run"}, nil))
				compiled, err := cm.Compile(id, nil)
				require.NoError(t, err)
				l := lua.NewState()
				defer l.Close()
				require.NoError(t, l.CallByParam(lua.P{Fn: l.LoadProto(compiled.Main), NRet: 1, Protect: true}))
				fn := l.Get(-1).(*lua.LTable).RawGetString("run").(*lua.LFunction)
				user := l.NewTable()
				require.Error(t, fn.Proto.CheckArguments(l, []lua.LValue{user}))
				user.RawSetString("name", lua.LString("ok"))
				require.NoError(t, fn.Proto.CheckArguments(l, []lua.LValue{user}))
			}
		})
	}
}

func TestArgumentContractsResolveImportedTypes(t *testing.T) {
	cm, err := NewCodeManager(zap.NewNop(), nil, Config{})
	require.NoError(t, err)
	libID := registry.NewID("test", "types")
	id := registry.NewID("test", "arguments")
	require.NoError(t, cm.AddNode(context.Background(), Node{ID: libID, Kind: api.Library, Source: `type User = {name: string}; return {}`}, nil))
	require.NoError(t, cm.AddNode(context.Background(), Node{ID: id, Kind: api.Function, Source: `return function(user: types.User) return user end`}, []Import{{ID: libID, Alias: "types"}}))
	compiled, err := cm.Compile(id, nil)
	require.NoError(t, err)
	l := lua.NewState()
	defer l.Close()
	require.NoError(t, l.CallByParam(lua.P{Fn: l.LoadProto(compiled.Main), NRet: 1, Protect: true}))
	fn := l.Get(-1).(*lua.LFunction)
	user := l.NewTable()
	require.Error(t, fn.Proto.CheckArguments(l, []lua.LValue{user}))
	user.RawSetString("name", lua.LString("ok"))
	require.NoError(t, fn.Proto.CheckArguments(l, []lua.LValue{user}))
}

// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/bytecode"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"go.uber.org/zap"
)

func compileArgumentEntry(t *testing.T, source string) *lua.FunctionProto {
	t.Helper()
	cm, err := code.NewCodeManager(zap.NewNop(), nil, code.Config{})
	require.NoError(t, err)
	id := registry.NewID("test", "arguments")
	require.NoError(t, cm.AddNode(context.Background(), code.Node{ID: id, Kind: luaapi.Function, Source: source, Method: "run"}, nil))
	compiled, err := cm.Compile(id, nil)
	require.NoError(t, err)
	return compiled.Main
}

func TestFunctionArgumentValidation(t *testing.T) {
	for _, dumped := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(map[bool]string{false: "source", true: "bytecode"}[dumped]+map[bool]string{false: "/process", true: "/function"}[enabled], func(t *testing.T) {
				proto := compileArgumentEntry(t, `local calls=0; return {run=function(id: string) calls=calls+1; return calls end}`)
				if dumped {
					blob, err := bytecode.Dump(proto)
					require.NoError(t, err)
					proto, err = bytecode.Undump(blob)
					require.NoError(t, err)
				}
				proc, err := NewFactory(FactoryConfig{Proto: proto, ValidateArguments: enabled})()
				require.NoError(t, err)
				defer proc.Close()
				ctx, fc := ctxapi.OpenFrameContext(context.Background())
				defer ctxapi.ReleaseFrameContext(fc)
				err = proc.Init(ctx, "run", payload.Payloads{payload.NewPayload(lua.LInteger(42), payload.Lua)})
				if enabled {
					require.Error(t, err)
					var apiErr apierror.Error
					require.ErrorAs(t, err, &apiErr)
					require.Equal(t, apierror.Invalid, apiErr.Kind())
				} else {
					require.NoError(t, err)
					var out process.StepOutput
					require.NoError(t, proc.Step(nil, &out))
				}
				ctx2, fc2 := ctxapi.OpenFrameContext(context.Background())
				defer ctxapi.ReleaseFrameContext(fc2)
				require.NoError(t, proc.Init(ctx2, "run", payload.Payloads{payload.NewPayload(lua.LString("ok"), payload.Lua)}))
				var out process.StepOutput
				require.NoError(t, proc.Step(nil, &out))
				want := lua.LInteger(1)
				if !enabled {
					want = 2
				}
				require.Equal(t, want, out.Result().Data(), "rejected method must not have run; pool remains reusable")
			})
		}
	}
}

func TestFunctionArgumentConversionFailureIsNotNil(t *testing.T) {
	proto := compileArgumentEntry(t, `return {run=function(id: string?) return true end}`)
	proc, err := NewFactory(FactoryConfig{Proto: proto, ValidateArguments: true})()
	require.NoError(t, err)
	defer proc.Close()
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	// No transcoder is available. This must not become an accepted optional nil.
	require.Error(t, proc.Init(ctx, "run", payload.Payloads{payload.NewPayload("value", payload.Format("unsupported"))}))
}

func TestUntypedFunctionArgumentsKeepExistingConversion(t *testing.T) {
	proto := compileArgumentEntry(t, `return {run=function(id) return id == nil end}`)
	proc, err := NewFactory(FactoryConfig{Proto: proto, ValidateArguments: true})()
	require.NoError(t, err)
	defer proc.Close()
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, proc.Init(ctx, "run", payload.Payloads{payload.NewPayload("value", payload.Format("unsupported"))}))
	var out process.StepOutput
	require.NoError(t, proc.Step(nil, &out))
	require.Equal(t, lua.LTrue, out.Result().Data())
}

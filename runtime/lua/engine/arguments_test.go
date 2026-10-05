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

// A deferred module can reject in Step, while a cached handler rejects in Init.
// Both paths must finish without entering the rejected handler body.
func executeArgumentEntry(ctx context.Context, t *testing.T, proc process.Process, method string, input payload.Payloads) (*process.StepOutput, error) {
	t.Helper()
	var out process.StepOutput
	if err := proc.Init(ctx, method, input); err != nil {
		return &out, err
	}
	for i := 0; i < 10000; i++ {
		out.Reset()
		err := proc.Step(nil, &out)
		if err != nil || out.Status() == process.StepDone {
			return &out, err
		}
	}
	t.Fatal("argument entry did not finish")
	return nil, nil
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
				_, err = executeArgumentEntry(ctx, t, proc, "run", payload.Payloads{payload.NewPayload(lua.LInteger(42), payload.Lua)})
				if enabled {
					require.Error(t, err)
					var apiErr apierror.Error
					require.ErrorAs(t, err, &apiErr)
					require.Equal(t, apierror.Invalid, apiErr.Kind())
				} else {
					require.NoError(t, err)
				}
				ctx2, fc2 := ctxapi.OpenFrameContext(context.Background())
				defer ctxapi.ReleaseFrameContext(fc2)
				out, err := executeArgumentEntry(ctx2, t, proc, "run", payload.Payloads{payload.NewPayload(lua.LString("ok"), payload.Lua)})
				require.NoError(t, err)
				want := lua.LInteger(1)
				if !enabled {
					want = 2
				}
				require.Equal(t, want, out.Result().Data(), "rejected method must not have run; pool remains reusable")
				ctx3, fc3 := ctxapi.OpenFrameContext(context.Background())
				defer ctxapi.ReleaseFrameContext(fc3)
				_, err = executeArgumentEntry(ctx3, t, proc, "run", payload.Payloads{payload.NewPayload(lua.LInteger(42), payload.Lua)})
				if enabled {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				ctx4, fc4 := ctxapi.OpenFrameContext(context.Background())
				defer ctxapi.ReleaseFrameContext(fc4)
				out, err = executeArgumentEntry(ctx4, t, proc, "run", payload.Payloads{payload.NewPayload(lua.LString("ok"), payload.Lua)})
				require.NoError(t, err)
				require.Equal(t, want*2, out.Result().Data(), "cached rejection must not run the body either")
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
	_, err = executeArgumentEntry(ctx, t, proc, "run", payload.Payloads{payload.NewPayload("value", payload.Format("unsupported"))})
	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, apierror.Invalid, apiErr.Kind())
	require.Equal(t, apierror.False, apiErr.Retryable())
	require.Equal(t, 1, apiErr.Details().GetInt("argument", 0))
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

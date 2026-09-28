// SPDX-License-Identifier: MPL-2.0

package process_test

import (
	"context"
	"strings"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"

	"github.com/stretchr/testify/require"
)

func TestTypedListenFiltersAtProcessDelivery(t *testing.T) {
	tc := setupProcessLeakTest(t, 2)
	defer tc.Close(t)
	frame, receiver := tc.frameCtxPID(t)
	script := `
 local process = require("process")
 type Request = {count: integer}
 local stream, err = process.listen("request", {message = true, type = Request})
 if not stream then return nil, tostring(err) end
 local self = process.pid()
 process.send(self, "request", {count = "invalid"})
 process.send(self, "request", {count = 17})
 local msg, ok = stream:receive()
 if not ok then return nil, "listener closed" end
 if msg:from() ~= tostring(self) then return nil, "sender changed" end
 if msg:topic() ~= "request" then return nil, "topic changed" end
 if msg:data().count ~= 17 then return nil, "unvalidated data delivered" end
 process.unlisten(stream)
 return 17
 `
	checker := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	manifest, diagnostics, err := checker.Check(script, "typed_listener", map[string]*io.Manifest{"process": processmod.ModuleTypes()})
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "diagnostics: %v", diagnostics)
	options, err := code.CompileOptionsForManifest(manifest)
	require.NoError(t, err)
	chunk, err := parse.Parse(strings.NewReader(script), "typed_listener")
	require.NoError(t, err)
	proto, err := lua.CompileWithOptions(chunk, "typed_listener", options)
	require.NoError(t, err)
	proc, err := engine.NewProcess(engine.WithProto(proto), engine.WithModuleBinder(func(l *lua.LState) error {
		engine.LoadModuleDef(l, engine.ChannelModule)
		mod, _ := processmod.Module.Build()
		l.SetGlobal("process", mod)
		return nil
	}))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(frame, 5*time.Second)
	defer cancel()
	result, err := tc.scheduler.Execute(ctx, receiver, proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script: %v", result.Error)
	require.Equal(t, int64(17), extractProcessInt64(result.Value.Data()))
}

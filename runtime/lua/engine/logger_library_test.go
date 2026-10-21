// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/modules/logger"
	"github.com/wippyai/runtime/system/scheduler/pool/static"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLibraryLoggerPooledExecutions(t *testing.T) {
	cm := setupFactoryCodeManager(t)
	loggerID := registry.NewID("", "logger")
	libID := registry.NewID("test", "logging")
	fnID := registry.NewID("test", "log_consumer")
	require.NoError(t, cm.AddNode(context.Background(), code.Node{
		ID: loggerID, Kind: luaapi.ModuleKind, Module: logger.Module,
	}, nil))
	require.NoError(t, cm.AddNode(context.Background(), code.Node{
		ID: libID, Kind: luaapi.Library,
		Source: `
			local log = require("logger")
			local named = log:named("library")
			local with = log:with({component = "library"})
			local chained = named:with({component = "library"}):named("child"):with({ready = true})
			local cached
			local calls = 0
			return function()
				calls = calls + 1
				if not cached then cached = log:named("cached"):with({cached = true}) end
				named:warn("named")
				with:warn("with")
				chained:warn("chained", {call = calls})
				cached:warn("cached")
				return calls
			end
		`,
	}, []code.Import{{ID: loggerID, Alias: "logger"}}))
	require.NoError(t, cm.AddNode(context.Background(), code.Node{
		ID: fnID, Kind: luaapi.Function, Method: "main",
		Source: `local emit = require("logging"); return function() return emit() end`,
	}, []code.Import{{ID: libID, Alias: "logging"}}))
	factory, err := NewProcessFactory(cm).CreateFactory(fnID)
	require.NoError(t, err)
	dispatcher := newPoolTestDispatcher()
	defer dispatcher.Stop()
	pool, err := static.New(factory, dispatcher, static.Config{Workers: 1})
	require.NoError(t, err)
	pool.Start()
	defer pool.Stop()

	for i := 1; i <= 3; i++ {
		core, output := observer.New(zap.DebugLevel)
		invocation := fmt.Sprintf("invocation-%d", i)
		base := zap.New(core).Named(invocation).With(zap.String("invocation", invocation))
		ctx, cancel := context.WithTimeout(logs.WithLogger(ctxapi.NewRootContext(), base), 5*time.Second)
		ctx, frame := ctxapi.OpenFrameContext(ctx)
		p := pid.PID{Host: "worker", UniqID: invocation}
		require.NoError(t, runtimeapi.SetFramePID(ctx, p))
		require.NoError(t, runtimeapi.SetFrameID(ctx, fnID))
		result, err := pool.Call(ctx, "main", nil)
		ctxapi.ReleaseFrameContext(frame)
		cancel()
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NoError(t, result.Error)
		entries := output.All()
		require.Len(t, entries, 4)
		for j, expected := range []struct {
			fields map[string]any
			name   string
		}{
			{nil, "library"},
			{map[string]any{"component": "library"}, ""},
			{map[string]any{"component": "library", "ready": true, "call": int64(i)}, "library.child"},
			{map[string]any{"cached": true}, "cached"},
		} {
			name := invocation
			if expected.name != "" {
				name += "." + expected.name
			}
			require.Equal(t, name, entries[j].LoggerName)
			fields := entries[j].ContextMap()
			require.Equal(t, invocation, fields["invocation"])
			require.Equal(t, p.String(), fields["pid"])
			require.Contains(t, fields["location"], fnID.String()+":")
			for key, value := range expected.fields {
				require.Equal(t, value, fields[key], key)
			}
		}
	}
}

// SPDX-License-Identifier: MPL-2.0

package process_test

import (
	"context"
	"testing"
	stdtime "time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	procapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	ttymod "github.com/wippyai/runtime/runtime/lua/modules/tty"
	ttysys "github.com/wippyai/runtime/system/tty"
)

type upgradeFactory struct {
	create func() (procapi.Process, error)
}

func (f *upgradeFactory) Create(registry.ID) (procapi.Process, *procapi.Meta, error) {
	proc, err := f.create()
	return proc, &procapi.Meta{Method: "main"}, err
}

func newTerminalProcess(t *testing.T, script string) *engine.Process {
	t.Helper()
	proto, err := lua.CompileString(script, "upgrade_terminal.lua")
	require.NoError(t, err)
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(func(l *lua.LState) error {
			mod, _ := processmod.Module.Build()
			l.SetGlobal("process", mod)
			return nil
		}),
		engine.WithModuleBinder(func(l *lua.LState) error {
			mod, _ := ttymod.Module.Build()
			l.SetGlobal("tty", mod)
			return nil
		}),
	)
	require.NoError(t, err)
	return proc
}

// TestUpgrade_KeepsTerminalAttachment upgrades a process in place and expects
// the new code to present on the viewport the process was started with.
func TestUpgrade_KeepsTerminalAttachment(t *testing.T) {
	tc := setupProcessLeakTest(t, 2)
	defer tc.Close(t)

	service := ttysys.NewService()
	defer service.Close()
	ttyapi.WithService(tc.ctx, service)

	ownerCtx, _ := tc.frameCtxPID(t)
	view, err := service.Create(ownerCtx, 20, 3)
	require.NoError(t, err)
	defer view.Close()
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)

	script := `
		local function main(state)
			if state == nil then
				process.upgrade("", "upgraded")
				return false
			end
			local surface, err = tty.surface()
			if not surface then error("surface: " .. tostring(err)) end
			local _, perr = surface:present({"after upgrade"})
			if perr then error("present: " .. tostring(perr)) end
			surface:close()
			return true
		end
		return { main = main }
	`
	proc := newTerminalProcess(t, script)
	factory := &upgradeFactory{create: func() (procapi.Process, error) {
		return newTerminalProcess(t, script), nil
	}}
	procapi.WithFactory(tc.ctx, factory)

	frameCtx, runPID := tc.frameCtxPID(t)
	require.NoError(t, ctxapi.FrameFromContext(frameCtx).SetMultiple(ttyapi.BindingPair(binding)))
	require.NoError(t, runtime.SetFrameID(frameCtx, registry.NewID("app", "terminal")))

	ctx, cancel := context.WithTimeout(frameCtx, 10*stdtime.Second)
	defer cancel()
	result, err := tc.scheduler.Execute(ctx, runPID, proc, "main", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "script error: %v", result.Error)
	require.Equal(t, "after upgrade", view.Snapshot().Rows[0])
}

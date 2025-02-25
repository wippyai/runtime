package models

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/uow"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestProgress(t *testing.T) {
	logger := zap.NewNop()

	loader := func(L *lua.LState) int {
		mod := L.NewTable()
		RegisterProgress(L, mod)
		L.Push(mod)
		return 1
	}

	ctx, uw := uow.OnContext(context.Background())
	defer func() { _ = uw.Close() }()

	t.Run("progress creation and configuration", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			-- Test default constructor
			local p1 = btea.progress({})
			assert(type(p1) == "userdata", "progress should be userdata")
			
			-- Test with custom width
			local p2 = btea.progress({
				width = 40
			})
			
			-- Test with gradient fill
			local p3 = btea.progress({
				fill_type = "gradient",
				gradient = {
					from = "#FF0000",
					to = "#00FF00"
				}
			})
			
			-- Test with solid fill
			local p4 = btea.progress({
				fill_type = "solid",
				color = "#0000FF"
			})
			
			-- Test without percentage
			local p5 = btea.progress({
				show_percentage = false
			})
		`, "test_progress_creation")

		require.NoError(t, err)
	})

	t.Run("progress operations", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			local p = btea.progress({})
			
			-- Test initial state
			assert(p:percent() == 0, "initial percent should be 0")
			
			-- Test setting percent
			local cmd = p:set_percent(0.5)
			assert(type(cmd) == "userdata", "set_percent should return a command")
			assert(math.abs(p:percent() - 0.5) < 1e-10, "percent should be approximately 0.5")
			
			-- Test increment
			cmd = p:incr_percent(0.1)
			assert(type(cmd) == "userdata", "incr_percent should return a command")
			assert(math.abs(p:percent() - 0.6) < 1e-10, "percent should be approximately 0.6")
			
			-- Test decrement
			cmd = p:decr_percent(0.2)
			assert(type(cmd) == "userdata", "decr_percent should return a command")
			assert(math.abs(p:percent() - 0.4) < 1e-10, "percent should be approximately 0.4")
			
			-- Test bounds
			cmd = p:set_percent(1.5) -- Should clamp to 1.0
			assert(math.abs(p:percent() - 1.0) < 1e-10, "percent should be approximately 1.0")
			
			cmd = p:set_percent(-0.5) -- Should clamp to 0.0
			assert(math.abs(p:percent() - 0.0) < 1e-10, "percent should be approximately 0.0")
		`, "test_progress_operations")

		require.NoError(t, err)
	})

	t.Run("progress view", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			local p = btea.progress({
				width = 20
			})
			
			-- Test view
			local view = p:view()
			assert(type(view) == "string", "view should return a string")
			
			-- Test view_as
			local view_half = p:view_as(0.5)
			assert(type(view_half) == "string", "view_as should return a string")
			assert(view_half ~= view, "view_as should differ from empty view")
			
			-- Test width affects rendering
			p:set_width(40)
			local wide_view = p:view()
			assert(#wide_view > #view, "wider progress bar should render longer")
		`, "test_progress_view")

		require.NoError(t, err)
	})
}

func TestProgressUpdate(t *testing.T) {
	logger := zap.NewNop()

	cvm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer cvm.Close()

	// Register the progress module
	mod := cvm.State().NewTable()
	RegisterProgress(cvm.State(), mod)
	cvm.State().SetGlobal("btea", mod)

	ctx, uw := uow.OnContext(context.Background())
	defer func() { _ = uw.Close() }()

	err = cvm.StartString(ctx, `
		local progress = btea.progress({})

		-- Set initial progress and get command
		local cmd = progress:set_percent(0.5)
		assert(cmd ~= nil, "set_percent should return a command")
		assert(progress:percent() == 0.5, "progress should be set to 0.5")
		
		-- Spawn initial view
		local initial_view = progress:view()
		
		-- Yield command and wait for its result
		local msg = coroutine.yield("ready_for_tick", cmd)
		
		-- Process the result
		cmd = progress:update(msg)
		assert(cmd ~= nil, "update should return next animation frame command")
		
		-- Verify view changes during animation
		local new_view = progress:view()
		assert(new_view ~= initial_view, "view should change during animation")
		
		-- Check animation state
		assert(progress:is_animating(), "progress should be animating")
		
		-- Yield next frame command
		coroutine.yield("next_frame", cmd)
		
		coroutine.yield("done")
	`, "test_progress_update")
	require.NoError(t, err)

	// First yield - after initial setup
	tasks, err := cvm.Step()
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	require.Equal(t, "ready_for_tick", tasks[0].Yielded[0].String())

	// Spawn command and execute it
	cmd, _ := protocol.UnwrapCommand(tasks[0].Yielded[1])
	msg := cmd()

	// Pass message back to Lua
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(msg)}

	// Step to process frame
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	require.Equal(t, "next_frame", tasks[0].Yielded[0].String())

	// Spawn next frame command
	cmd, _ = protocol.UnwrapCommand(tasks[0].Yielded[1])
	msg = cmd()

	// Pass message back to Lua
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(msg)}

	// Final step
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	require.Equal(t, "done", tasks[0].Yielded[0].String())
}

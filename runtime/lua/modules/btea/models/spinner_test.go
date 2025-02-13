package models

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestSpinner(t *testing.T) {
	logger := zap.NewNop()

	loader := func(L *lua.LState) int {
		mod := L.NewTable()
		RegisterSpinner(L, mod)
		L.Push(mod)
		return 1
	}

	t.Run("spinner creation and basic operations", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local btea = require("btea")
			
			-- Spawn a basic spinner
			local spinner = btea.spinner({
				type = btea.spinners.LINE,
				interval = "100ms"
			})
			
			-- Test initial state
			assert(type(spinner) == "userdata", "spinner should be userdata")
			
			-- Test view method
			local view = spinner:view()
			assert(type(view) == "string", "view should return a string")
			
			-- Test tick method
			local cmd = spinner:tick()
			assert(type(cmd) == "userdata", "tick should return a command")
		`, "test_spinner")

		require.NoError(t, err)
	})

	t.Run("spinner types", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local btea = require("btea")
			
			local spinner_types = {
				"LINE",
				"DOT",
				"MINIDOT",
				"JUMP",
				"PULSE",
				"POINTS",
				"GLOBE",
				"MOON",
				"MONKEY",
				"METER",
				"HAMBURGER",
				"ELLIPSIS"
			}
			
			for _, type_name in ipairs(spinner_types) do
				local spinner = btea.spinner({
					type = btea.spinners[type_name]
				})
				assert(type(spinner) == "userdata", type_name .. " spinner creation failed")
				
				local view = spinner:view()
				assert(type(view) == "string", type_name .. " view failed")
				
				local cmd = spinner:tick()
				assert(type(cmd) == "userdata", type_name .. " tick failed")
			end
		`, "test_spinner_types")

		require.NoError(t, err)
	})

	t.Run("spinner interval parsing", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local btea = require("btea")
			
			local intervals = {
				"100ms",
				"1s",
				"500",  -- numeric milliseconds
				100     -- numeric value
			}
			
			for _, interval in ipairs(intervals) do
				local spinner = btea.spinner({
					type = btea.spinners.LINE,
					interval = interval
				})
				assert(type(spinner) == "userdata", "spinner creation failed for interval: " .. tostring(interval))
				spinner:set_interval(interval)
			end
		`, "test_spinner_intervals")

		require.NoError(t, err)
	})

	t.Run("spinner error handling", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test invalid interval during creation
		err = vm.DoString(nil, `
			local btea = require("btea")
			
			-- Test invalid interval
			local ok, err = pcall(function()
				local spinner = btea.spinner({
					interval = "invalid_interval"
				})
			end)
			
			if ok then
				error("Expected spinner creation with invalid interval to fail")
			end
			
			-- Test negative interval during set_interval
			local spinner = btea.spinner({})  -- Spawn with default interval
			ok, err = pcall(function()
				spinner:set_interval("-100ms")
			end)
			
			if ok then
				error("Expected set_interval with negative interval to fail")
			end
			
			-- Test invalid type during set_interval
			ok, err = pcall(function()
				spinner:set_interval({})  -- Pass a table instead of a string/number
			end)
			
			if ok then
				error("Expected set_interval with invalid type to fail")
			end
		`, "test_spinner_errors")

		require.Error(t, err, "Expected error handling tests to fail appropriately")
	})
}

func TestSpinnerUpdate(t *testing.T) {
	logger := zap.NewNop()

	cvm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer cvm.Close()

	// Register the spinner module
	mod := cvm.State().NewTable()
	RegisterSpinner(cvm.State(), mod)
	cvm.State().SetGlobal("btea", mod)

	err = cvm.StartString(context.Background(), `
        local spinner = btea.spinner({
            type = btea.spinners.LINE,
            interval = "100ms"
        })

        -- Test update with nil
        local cmd = spinner:tick()
        assert(cmd ~= nil, "cmd should not be nil")
        
		local orig_view = spinner:view()

        -- Yield initial cmd and wait for tick message
        local msg = coroutine.yield("ready_for_tick", cmd)
        
        -- Process tick message
        cmd = spinner:update(msg)
        assert(cmd ~= nil, "cmd should not be nil after tick")
        local view = spinner:view()
        assert(type(view) == "string", "view should return a string")
		assert(view ~= orig_view, "view should change after tick")

        coroutine.yield("done")
    `, "test_spinner_update")
	require.NoError(t, err)

	// First yield - after nil update
	tasks, err := cvm.Step()
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	require.Equal(t, "ready_for_tick", tasks[0].Yielded[0].String())

	cmd := protocol.UnwrapCommand(cvm.State(), tasks[0].Yielded[1])

	// Spawn a tick message using protocol
	tickMsg := protocol.MsgToLua(cmd())
	tasks[0].Resumed = []lua.LValue{tickMsg}

	//// Step to process tick
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	require.Equal(t, "done", tasks[0].Yielded[0].String())
}

package models

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestViewport(t *testing.T) {
	logger := zap.NewNop()

	loader := func(L *lua.LState) int {
		mod := L.NewTable()
		RegisterViewport(L, mod)
		L.Push(mod)
		return 1
	}

	t.Run("viewport creation and configuration", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			-- Test default constructor
			local v1 = btea.viewport({
				width = 80,
				height = 40
			})
			assert(type(v1) == "userdata", "viewport should be userdata")
			
			-- Test width and height getters
			assert(v1:width() == 80, "viewport width should be 80")
			assert(v1:height() == 40, "viewport height should be 40")
			
			-- Test content setting
			v1:set_content("Hello, World!")
			local view = v1:view()
			assert(type(view) == "string", "view should return a string")
			
			-- Test with mouse wheel options
			local v2 = btea.viewport({
				width = 60,
				height = 30,
				mouse_wheel_enabled = true,
				mouse_wheel_delta = 3
			})
			
			-- Test high performance mode
			local v3 = btea.viewport({
				width = 40,
				height = 20,
				high_performance = true
			})
		`, "test_viewport_creation")

		require.NoError(t, err)
	})

	t.Run("viewport scrolling operations", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			local viewport = btea.viewport({
				width = 40,
				height = 10
			})
			
			-- Set multi-line content
			local content = "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\nLine 6\nLine 7\nLine 8\nLine 9\nLine 10\nLine 11\nLine 12"
			viewport:set_content(content)
			
			-- Test initial position
			assert(viewport:at_top(), "should start at top")
			assert(not viewport:at_bottom(), "should not be at bottom initially")
			assert(viewport:y_offset() == 0, "initial y_offset should be 0")
			
			-- Test line navigation
			viewport:line_down(1)
			assert(not viewport:at_top(), "should not be at top after line_down")
			assert(viewport:y_offset() > 0, "y_offset should be positive after line_down")
			
			viewport:line_up(1)
			assert(viewport:at_top(), "should be back at top after line_up")
			
			-- Test page navigation
			viewport:page_down()
			assert(not viewport:at_top(), "should not be at top after page_down")
			
			viewport:page_up()
			assert(viewport:at_top(), "should be back at top after page_up")
			
			-- Test half page navigation
			viewport:half_page_down()
			local mid_offset = viewport:y_offset()
			assert(mid_offset > 0, "offset should be positive after half_page_down")
			
			viewport:half_page_up()
			assert(viewport:y_offset() < mid_offset, "offset should decrease after half_page_up")
			
			-- Test scroll to extremes
			viewport:scroll_to_top()
			assert(viewport:at_top(), "should be at top after scroll_to_top")
			
			viewport:scroll_to_bottom()
			assert(viewport:at_bottom(), "should be at bottom after scroll_to_bottom")
			
			-- Test scroll percentage
			local percent = viewport:scroll_percent()
			assert(percent >= 0 and percent <= 100, "scroll percentage should be between 0 and 100")
		`, "test_viewport_scrolling")

		require.NoError(t, err)
	})

	t.Run("viewport content info", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			local viewport = btea.viewport({
				width = 40,
				height = 10
			})
			
			-- Set content and check line counts
			local content = "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
			viewport:set_content(content)
			
			local total = viewport:total_lines()
			assert(total == 5, "total_lines should return correct count")
			
			local visible = viewport:visible_lines()
			assert(visible <= total, "visible_lines should not exceed total_lines")
		`, "test_viewport_content_info")

		require.NoError(t, err)
	})
}

func TestViewportMouseInteraction(t *testing.T) {
	logger := zap.NewNop()

	cvm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer cvm.Close()

	// Register the viewport module
	mod := cvm.State().NewTable()
	RegisterViewport(cvm.State(), mod)
	cvm.State().SetGlobal("btea", mod)

	uw, ctx := engine.NewUnitOfWork(context.Background(), cvm.State())
	defer func() { _ = uw.Close() }()

	err = cvm.StartString(ctx, `
		local viewport = btea.viewport({
			width = 40,
			height = 5,
			mouse_wheel_enabled = true,
			mouse_wheel_delta = 2
		})

		-- Set multi-line content that's larger than viewport
		viewport:set_content("Line 1\nLine 2\nLine 3\nLine 4\nLine 5\nLine 6\nLine 7\nLine 8\nLine 9\nLine 10")
		local initial_offset = viewport:y_offset()
		
		-- Test mouse wheel down (scrolls up)
		local msg = coroutine.yield("wheel_down", nil)
		local cmd = viewport:update(msg)
		local new_offset = viewport:y_offset()
		assert(new_offset > initial_offset, "y_offset should increase after wheel down")
		
		-- Test mouse wheel up (scrolls down)
		msg = coroutine.yield("wheel_up", cmd)
		cmd = viewport:update(msg)
		assert(viewport:y_offset() < new_offset, "y_offset should decrease after wheel up")
		
		-- Disable mouse wheel
		viewport:enable_mouse(false)
		local frozen_offset = viewport:y_offset()
		msg = coroutine.yield("wheel_disabled", cmd)
		cmd = viewport:update(msg)
		assert(viewport:y_offset() == frozen_offset, "scroll should not change when mouse wheel is disabled")
		
		coroutine.yield("done", cmd)
	`, "test_viewport_mouse")
	require.NoError(t, err)

	// First yield - wheel down
	tasks, err := cvm.Step()
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	require.Equal(t, "wheel_down", tasks[0].Yielded[0].String())

	// send wheel down event (scrolls up)
	wheelDownMsg := protocol.MsgToLua(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
		Y:      1,
	})
	tasks[0].Resumed = []lua.LValue{wheelDownMsg}

	// Process wheel down
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "wheel_up", tasks[0].Yielded[0].String())

	// send wheel up event (scrolls down)
	wheelUpMsg := protocol.MsgToLua(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
		Y:      -1,
	})
	tasks[0].Resumed = []lua.LValue{wheelUpMsg}

	// Process wheel up
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "wheel_disabled", tasks[0].Yielded[0].String())

	// send wheel event while disabled
	wheelDisabledMsg := protocol.MsgToLua(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
		Y:      1,
	})
	tasks[0].Resumed = []lua.LValue{wheelDisabledMsg}

	// Final step
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "done", tasks[0].Yielded[0].String())
}

func TestViewportUpdate(t *testing.T) {
	logger := zap.NewNop()

	cvm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer cvm.Close()

	// Register the viewport module
	mod := cvm.State().NewTable()
	RegisterViewport(cvm.State(), mod)
	cvm.State().SetGlobal("btea", mod)

	uw, ctx := engine.NewUnitOfWork(context.Background(), cvm.State())
	defer func() { _ = uw.Close() }()

	err = cvm.StartString(ctx, `
		local viewport = btea.viewport({
			width = 40,
			height = 10,
			high_performance = true
		})

		-- Set initial content
		viewport:set_content("Line 1\nLine 2\nLine 3\nLine 4\nLine 5")
		local initial_view = viewport:view()
		
		-- Test dimension updates
		viewport:set_width(60)
		viewport:set_height(15)
		assert(viewport:width() == 60, "width should update")
		assert(viewport:height() == 15, "height should update")
		
		-- Test scrolling update
		viewport:line_down(1)
		local msg = coroutine.yield("ready_for_update", nil)
		local cmd = viewport:update(msg)
		
		coroutine.yield("done", cmd)
	`, "test_viewport_update")
	require.NoError(t, err)

	// First yield - after dimension updates
	tasks, err := cvm.Step()
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	require.Equal(t, "ready_for_update", tasks[0].Yielded[0].String())

	// send update message
	updateMsg := protocol.MsgToLua(tea.KeyMsg{Type: tea.KeySpace})
	tasks[0].Resumed = []lua.LValue{updateMsg}

	// Step to process update
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	require.Equal(t, "done", tasks[0].Yielded[0].String())
}

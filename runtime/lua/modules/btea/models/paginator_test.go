package models

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/uow"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestPaginator(t *testing.T) {
	logger := zap.NewNop()

	loader := func(L *lua.LState) int {
		mod := L.NewTable()
		RegisterPaginator(L, mod)
		L.Push(mod)
		return 1
	}

	t.Run("paginator creation and configuration", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx, uw := uow.WithContext(context.Background())
		defer func() { _ = uw.Close() }()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			-- Test default constructor
			local p1 = btea.paginator({})
			assert(type(p1) == "userdata", "paginator should be userdata")
			
			-- Test with custom options
			local p2 = btea.paginator({
				type = btea.paginator_types.ARABIC,
				page = 2,
				per_page = 10,
				total_pages = 5
			})
			
			-- Test current page
			assert(p2:get_current_page() == 2, "current page should be 2")
			
			-- Test per page setting
			p2:set_per_page(20)
			
			-- Test type setting
			p2:set_type(btea.paginator_types.DOTS)
			assert(p2:get_type() == btea.paginator_types.DOTS, "type should be DOTS")
		`, "test_paginator_creation")

		require.NoError(t, err)
	})

	t.Run("paginator navigation and bounds", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx, uw := uow.WithContext(context.Background())
		defer func() { _ = uw.Close() }()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			-- Spawn paginator with 3 pages
			local p = btea.paginator({
				total_pages = 3
			})
			
			-- Test initial state
			local current_page = p:get_current_page()
			assert(current_page == 0, "should start at page 0")
			assert(p:on_first_page(), "should be on first page")
			assert(not p:on_last_page(), "should not be on last page")
			
			-- Test next page
			p:next_page()
			current_page = p:get_current_page()
			assert(current_page == 1, "should move to page 1")
			assert(not p:on_first_page(), "should not be on first page")
			
			-- Test prev page
			p:prev_page()
			current_page = p:get_current_page()
			assert(current_page == 0, "should move back to page 0")
			
			-- Test bounds at start
			p:prev_page() -- Should stay on page 0
			assert(p:get_current_page() == 0, "should remain on page 0")
			
			-- Go to last page
			p:next_page()
			p:next_page()
			p:next_page()
			assert(p:on_last_page(), "should be on last page")
			
			-- Test bounds at end
			local last_page = p:get_current_page()
			p:next_page() -- Should stay on last page
			assert(p:get_current_page() == last_page, "should remain on last page")
		`, "test_paginator_navigation")

		require.NoError(t, err)
	})

	t.Run("paginator slice operations", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx, uw := uow.WithContext(context.Background())
		defer func() { _ = uw.Close() }()

		err = vm.DoString(ctx, `
			local btea = require("btea")
			
			local p = btea.paginator({
				per_page = 10,
				total_pages = 3
			})
			
			-- Test items on page
			local items = p:items_on_page(25)
			assert(items == 10, "should have 10 items on page")
			
			-- Test slice bounds
			local start_idx, end_idx = p:get_slice_bounds(25)
			assert(start_idx == 0, "slice should start at 0")
			assert(end_idx == 10, "slice should end at 10")
			
			-- Move to second page and test bounds
			p:next_page()
			start_idx, end_idx = p:get_slice_bounds(25)
			assert(start_idx == 10, "slice should start at 10")
			assert(end_idx == 20, "slice should end at 20")
			
			-- Test last page with partial items
			p:next_page()
			start_idx, end_idx = p:get_slice_bounds(25)
			assert(start_idx == 20, "slice should start at 20")
			assert(end_idx == 25, "slice should end at total items")
		`, "test_paginator_slices")

		require.NoError(t, err)
	})
}

func TestPaginatorUpdate(t *testing.T) {
	logger := zap.NewNop()

	cvm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer cvm.Close()

	// Register the paginator module
	mod := cvm.State().NewTable()
	RegisterPaginator(cvm.State(), mod)
	cvm.State().SetGlobal("btea", mod)

	ctx, uw := uow.WithContext(context.Background())
	defer func() { _ = uw.Close() }()

	err = cvm.StartString(ctx, `
		local paginator = btea.paginator({
			total_pages = 3
		})

		-- Test initial state
		local initial_page = paginator:get_current_page()
		assert(initial_page == 0, "should start on page 0")
		
		-- Yield for navigation message
		local msg = coroutine.yield("ready_for_next", nil)
		
		-- Process the navigation message
		local cmd = paginator:update(msg)
		
		-- Verify state after navigation
		local new_page = paginator:get_current_page()
		assert(new_page == 1, "page should be 1 after navigation")
		
		coroutine.yield("done", cmd)
	`, "test_paginator_update")
	require.NoError(t, err)

	// First yield - after initial state
	tasks, err := cvm.Step()
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	require.Equal(t, "ready_for_next", tasks[0].Yielded[0].String())

	// Spawn a navigation message using protocol
	navMsg := protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyRight})
	tasks[0].Resumed = []lua.LValue{navMsg}

	// Step to process navigation
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, 1, len(tasks))
	require.Equal(t, "done", tasks[0].Yielded[0].String())
}

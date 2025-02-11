package models

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestTable(t *testing.T) {
	logger := zap.NewNop()

	loader := func(L *lua.LState) int {
		mod := L.NewTable()
		RegisterTable(L, mod)
		protocol.RegisterKeyBinding(L, mod) // Register key bindings
		render.RegisterStyle(L, mod)        // Register styles
		L.Push(mod)
		return 1
	}

	t.Run("table creation and basic configuration", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			-- Test default constructor
			local tbl = btea.table({})
			assert(type(tbl) == "userdata", "table should be userdata")
			
			-- Test with columns and rows
			local tbl2 = btea.table({
				cols = {
					{title = "Alias", width = 10},
					{title = "Age", width = 5},
					{title = "City", width = 15}
				},
				rows = {
					{"Alice", "25", "New York"},
					{"Bob", "30", "San Francisco"},
					{"Charlie", "35", "Chicago"}
				},
				width = 40,
				height = 10,
				focused = true
			})
			
			-- Test setting dimensions
			tbl2:set_width(40)
			tbl2:set_height(10)
			
			-- Test view methods
			local view = tbl2:view()
			assert(type(view) == "string", "view should return a string")
			
			local help = tbl2:help_view()
			assert(type(help) == "string", "help_view should return a string")
		`, "test_table_basic")

		require.NoError(t, err)
	})

	t.Run("table styling", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			local tbl = btea.table({
				cols = {
					{title = "Alias", width = 10},
					{title = "Age", width = 5}
				},
				rows = {
					{"Alice", "25"},
					{"Bob", "30"}
				},
				styles = {
					header = btea.style():bold():foreground("#00FF00"),
					cell = btea.style():foreground("#FFFFFF"),
					selected = btea.style():foreground("#FFFF00"):background("#333333")
				}
			})
			
			-- Check that view works with styles
			local view = tbl:view()
			assert(type(view) == "string", "view with styles should return string")
		`, "test_table_styling")

		require.NoError(t, err)
	})

	t.Run("table row operations", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			local tbl = btea.table({
				cols = {
					{title = "Alias", width = 10},
					{title = "Age", width = 5}
				},
				rows = {
					{"Alice", "25"},
					{"Bob", "30"},
					{"Charlie", "35"}
				}
			})
			
			-- Test row selection
			tbl:set_cursor(1)
			assert(tbl:cursor() == 1, "cursor should be at position 1")
			
			local selected = tbl:selected_row()
			assert(selected[1] == "Bob", "selected row should be Bob's row")
			
			-- Test row movement
			tbl:move_up()
			assert(tbl:cursor() == 0, "cursor should move up to 0")
			
			tbl:move_down(2)
			assert(tbl:cursor() == 2, "cursor should move down to 2")
			
			-- Test goto operations
			tbl:goto_top()
			assert(tbl:cursor() == 0, "cursor should be at top")
			
			tbl:goto_bottom()
			assert(tbl:cursor() == 2, "cursor should be at bottom")
			
			-- Test row updates
			local new_rows = {
				{"Dave", "40"},
				{"Eve", "45"}
			}
			tbl:set_rows(new_rows)
			
			local rows = tbl:get_rows()
			assert(#rows == 2, "should have 2 rows after update")
			assert(rows[1][1] == "Dave", "first row should be Dave")
		`, "test_table_rows")

		require.NoError(t, err)
	})

	t.Run("table column operations", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			local tbl = btea.table({
				cols = {
					{title = "Alias", width = 10},
					{title = "Age", width = 5}
				}
			})
			
			-- Test column getters
			local cols = tbl:get_columns()
			assert(#cols == 2, "should have 2 columns")
			assert(cols[1].title == "Alias", "first column should be Alias")
			
			-- Test column updates
			local new_cols = {
				{title = "First", width = 15},
				{title = "Last", width = 15},
				{title = "Email", width = 25}
			}
			tbl:set_columns(new_cols)
			
			cols = tbl:get_columns()
			assert(#cols == 3, "should have 3 columns after update")
			assert(cols[3].title == "Email", "third column should be Email")
		`, "test_table_columns")

		require.NoError(t, err)
	})

	t.Run("table from values", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local btea = require("btea")
			
			local tbl = btea.table({
				cols = {
					{title = "Alias", width = 10},
					{title = "Age", width = 5},
					{title = "City", width = 15}
				}
			})
			
			-- Test from_values with default separator
			tbl:from_values("Alice,25,New York\nBob,30,San Francisco")
			
			local rows = tbl:get_rows()
			assert(#rows == 2, "should have 2 rows")
			assert(rows[1][3] == "New York", "should parse city correctly")
			
			-- Test from_values with custom separator
			tbl:from_values("Dave;35;Chicago\nEve;40;Boston", ";")
			
			rows = tbl:get_rows()
			assert(#rows == 2, "should have 2 rows after update")
			assert(rows[2][3] == "Boston", "should parse with custom separator")
		`, "test_table_from_values")

		require.NoError(t, err)
	})
}

func TestTableUpdate(t *testing.T) {
	logger := zap.NewNop()

	cvm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer cvm.Close()

	// Register the table module
	mod := cvm.State().NewTable()
	RegisterTable(cvm.State(), mod)
	protocol.RegisterKeyBinding(cvm.State(), mod)
	render.RegisterStyle(cvm.State(), mod)
	cvm.State().SetGlobal("btea", mod)

	err = cvm.StartString(context.Background(), `
		local table = btea.table({
			cols = {
				{title = "Alias", width = 10},
				{title = "Age", width = 5}
			},
			rows = {
				{"Alice", "25"},
				{"Bob", "30"},
				{"Charlie", "35"}
			}
		})

		-- Initial state
		assert(table:cursor() == 0, "should start at first row")
		
		-- Focus the table
		table:focus()
		
		-- Yield for input message
		local msg = coroutine.yield("ready_for_input", nil)
		
		-- Process key down
		local cmd = table:update(msg)
		assert(table:cursor() == 1, "cursor should move down")
		
		-- Yield for next input
		msg = coroutine.yield("ready_for_blur", cmd)
		
		-- Process blur
		table:blur()
		cmd = table:update(msg)
		
		coroutine.yield("done", cmd)
	`, "test_table_update")
	require.NoError(t, err)

	// First yield - after focus
	tasks, err := cvm.Step()
	require.NoError(t, err)
	require.Equal(t, "ready_for_input", tasks[0].Yielded[0].String())

	// Simulate down arrow key
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyDown})}
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "ready_for_blur", tasks[0].Yielded[0].String())

	// Simulate blur
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyEsc})}
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "done", tasks[0].Yielded[0].String())
}

package list

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func setupListTest(t *testing.T) *engine.VM {
	logger := zap.NewNop()

	// Setup loader with all required dependencies
	loader := func(L *lua.LState) int {
		mod := L.NewTable()
		RegisterList(L, mod)
		protocol.RegisterKeyBinding(L, mod)
		render.RegisterStyle(L, mod)
		L.Push(mod)
		return 1
	}

	vm, err := engine.NewVM(logger, engine.WithLoader("btea", loader))
	require.NoError(t, err)
	return vm
}

func TestList(t *testing.T) {
	ctx := context.Background()

	// Basic initialization and configuration
	t.Run("list creation and basic operations", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            -- Create basic list
            local list = btea.new_list({
                width = 40,
                height = 20,
                title = "Test List",
                items = {
                    {
                        title = "Item 1",
                        description = "First item",
                        filter_value = "item1"
                    },
                    {
                        title = "Item 2",
                        description = "Second item",
                        filter_value = "item2"
                    }
                }
            })

            -- Test basic properties
            assert(list ~= nil, "list should be created")
            assert(#list:items() == 2, "should have 2 items")
            assert(list:cursor() == 0, "cursor should start at 0")
            
            -- Test cursor movement
            list:cursor_down()
            assert(list:cursor() == 1, "cursor should move down")
            
            list:cursor_up()
            assert(list:cursor() == 0, "cursor should move back up")

            -- Test item operations
            list:set_items({
                {
                    title = "New Item",
                    description = "Added item",
                    filter_value = "new"
                }
            })
            assert(#list:items() == 1, "should update to 1 item")

            local selected = list:selected_item()
            assert(selected.title == "New Item", "should select new item")

            -- Test view generation
            local view = list:view()
            assert(type(view) == "string", "view should generate string")
        `, "test_list_basic")

		require.NoError(t, err)
	})

	// Test pagination behavior
	t.Run("list pagination", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            -- Create list with multiple pages
            local list = btea.new_list({
                width = 40,
                height = 10,  -- Small height to force pagination
                items = {
                    { title = "Item 1", filter_value = "1" },
                    { title = "Item 2", filter_value = "2" },
                    { title = "Item 3", filter_value = "3" },
                    { title = "Item 4", filter_value = "4" },
                    { title = "Item 5", filter_value = "5" }
                }
            })
            
            -- Test page navigation
            assert(not list:is_filtered(), "should not start filtered")
            list:next_page()
            list:prev_page()
            
            -- Test cursor bounds
            local initial_cursor = list:cursor()
            for i = 1, 10 do
                list:cursor_down()
            end
            assert(list:cursor() < #list:items(), "cursor should not exceed item count")
            
            for i = 1, 10 do
                list:cursor_up()
            end
            assert(list:cursor() >= 0, "cursor should not go below zero")
        `, "test_list_pagination")

		require.NoError(t, err)
	})

	// Test filtering functionality
	t.Run("list filtering", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.new_list({
                width = 40,
                height = 20,
                items = {
                    { title = "Apple", filter_value = "apple" },
                    { title = "Banana", filter_value = "banana" },
                    { title = "Orange", filter_value = "orange" }
                }
            })
            
            -- Test filter state
            assert(not list:is_filtered(), "should start unfiltered")
            assert(list:filter_state() == "unfiltered", "should report unfiltered state")
            
            -- Test filter value
            assert(list:filter_value() == "", "should start with empty filter")
            
            -- Test filter enable/disable
            list:set_filtering_enabled(false)
            assert(not list:filtering_enabled(), "should disable filtering")
            
            list:set_filtering_enabled(true)
            assert(list:filtering_enabled(), "should enable filtering")
        `, "test_list_filtering")

		require.NoError(t, err)
	})

	// Test display options
	t.Run("list display options", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.new_list({
                width = 40,
                height = 20,
                show_title = true,
                show_filter = true,
                show_status_bar = true,
                show_pagination = true,
                show_help = true
            })
            
            -- Test visibility toggles
            list:set_show_title(false)
            assert(not list:show_title(), "should hide title")
            
            list:set_show_filter(false)
            assert(not list:show_filter(), "should hide filter")
            
            list:set_show_status_bar(false)
            assert(not list:show_status_bar(), "should hide status bar")
            
            list:set_show_pagination(false)
            assert(not list:show_pagination(), "should hide pagination")
            
            list:set_show_help(false)
            assert(not list:show_help(), "should hide help")
            
            -- Test dimension updates
            list:set_width(60)
            list:set_height(30)
            assert(list:width() == 60, "should update width")
            assert(list:height() == 30, "should update height")
        `, "test_list_display")

		require.NoError(t, err)
	})
}

// Test message handling and updates
func TestListUpdate(t *testing.T) {
	logger := zap.NewNop()

	cvm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer cvm.Close()

	// Register the list module
	mod := cvm.State().NewTable()
	RegisterList(cvm.State(), mod)
	cvm.State().SetGlobal("btea", mod)

	err = cvm.StartString(context.Background(), `
        local list = btea.new_list({
            width = 40,
            height = 20,
            items = {
                { title = "Item 1", filter_value = "1" },
                { title = "Item 2", filter_value = "2" }
            }
        })

        -- Get initial state
        local initial_cursor = list:cursor()
        assert(initial_cursor == 0, "should start at first item")
        
        -- Process cursor down message
        local msg = coroutine.yield("cursor_down", nil)
        local cmd = list:update(msg)
        assert(list:cursor() == 1, "cursor should move down")
        
        -- Process cursor up message
        msg = coroutine.yield("cursor_up", cmd)
        cmd = list:update(msg)
        assert(list:cursor() == 0, "cursor should move up")
        
        coroutine.yield("done", cmd)
    `, "test_list_update")
	require.NoError(t, err)

	// First yield - after initial state
	tasks, err := cvm.Step()
	require.NoError(t, err)
	require.Equal(t, "cursor_down", tasks[0].Yielded[0].String())

	// Simulate cursor down
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyDown})}
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "cursor_up", tasks[0].Yielded[0].String())

	// Simulate cursor up
	tasks[0].Resumed = []lua.LValue{protocol.MsgToLua(tea.KeyMsg{Type: tea.KeyUp})}
	tasks, err = cvm.Step(tasks[0])
	require.NoError(t, err)
	require.Equal(t, "done", tasks[0].Yielded[0].String())
}

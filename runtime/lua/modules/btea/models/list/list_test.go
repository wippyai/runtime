package list

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/uow"
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
	ctx, uw := uow.OnContext(ctx)
	defer func() { _ = uw.Close() }()

	// Basic initialization and configuration
	t.Run("list creation and basic operations", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            -- Spawn basic list
            local list = btea.list({
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
            
            -- Spawn list with multiple pages
            local list = btea.list({
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
            
            local list = btea.list({
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
            
            local list = btea.list({
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

	ctx, uw := uow.OnContext(context.Background())
	defer func() { _ = uw.Close() }()

	err = cvm.StartString(ctx, `
        local list = btea.list({
            width = 40,
            height = 20,
            items = {
                { title = "Item 1", filter_value = "1" },
                { title = "Item 2", filter_value = "2" }
            }
        })

        -- Spawn initial state
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

func TestItemManagement(t *testing.T) {
	ctx := context.Background()

	// Test empty list behavior
	t.Run("empty list operations", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            -- Spawn empty list
            local list = btea.list({
                width = 40,
                height = 20
            })
            
            -- Test empty list state
            assert(#list:items() == 0, "should start empty")
            assert(list:selected_item() == nil, "should have no selection")
            assert(list:cursor() == 0, "cursor should be at 0")
            
            -- AddCleanup first item
            list:insert_item(0, {
                title = "First Item",
                filter_value = "first"
            })
            assert(#list:items() == 1, "should have one item")
            assert(list:selected_item().title == "First Item", "should select first item")
        `, "test_empty_list")

		require.NoError(t, err)
	})

	// Test single item operations
	t.Run("single item operations", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20,
                items = {
                    {
                        title = "Single Item",
                        filter_value = "single"
                    }
                }
            })
            
            -- Test single item state
            assert(#list:items() == 1, "should have one item")
            assert(list:cursor() == 0, "cursor should be at first item")
            
            -- Test item removal
            list:remove_item(0)
            assert(#list:items() == 0, "should be empty after removal")
            assert(list:selected_item() == nil, "should have no selection")
        `, "test_single_item")

		require.NoError(t, err)
	})

	// Test item insertion and removal at different positions
	t.Run("item position operations", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20,
                items = {
                    { title = "Item 1", filter_value = "1" },
                    { title = "Item 2", filter_value = "2" }
                }
            })
            
            -- Test middle insertion
            list:insert_item(1, {
                title = "Middle Item",
                filter_value = "middle"
            })
            assert(#list:items() == 3, "should have three items")
            assert(list:items()[2].title == "Middle Item", "middle item should be at index 1")
            
            -- Test end insertion
            list:insert_item(3, {
                title = "End Item",
                filter_value = "end"
            })
            assert(#list:items() == 4, "should have four items")
            assert(list:items()[4].title == "End Item", "end item should be last")
            
            -- Test beginning insertion
            list:insert_item(0, {
                title = "Launch Item",
                filter_value = "start"
            })
            assert(#list:items() == 5, "should have five items")
            assert(list:items()[1].title == "Launch Item", "start item should be first")
            
            -- Test removal from different positions
            list:remove_item(2)  -- Remove from middle
            assert(#list:items() == 4, "should have four items after middle removal")
            
            list:remove_item(0)  -- Remove from start
            assert(#list:items() == 3, "should have three items after start removal")
            
            list:remove_item(2)  -- Remove from end
            assert(#list:items() == 2, "should have two items after end removal")
        `, "test_item_positions")

		require.NoError(t, err)
	})

	// Test item content validation
	t.Run("item content validation", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20
            })
            
            -- Test valid item
            list:insert_item(0, {
                title = "Valid Item",
                description = "Description",
                filter_value = "valid"
            })
            assert(#list:items() == 1, "should accept valid item")
            
            -- Test item with missing optional fields
            list:insert_item(1, {
                filter_value = "minimal"  -- Only required field
            })
            assert(#list:items() == 2, "should accept minimal item")
            
            -- Spawn the minimal item
            local minimal_item = list:items()[2]
            -- The title/description can be nil or an empty string, what matters is that we can still use the item
            assert(minimal_item.filter_value == "minimal", "should preserve filter value for minimal item")
            local view = list:view()  -- Should not error when rendering minimal item
        `, "test_item_content")

		require.NoError(t, err)
	})

	// Test invalid index handling
	t.Run("invalid index handling", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20,
                items = {
                    { title = "Item 1", filter_value = "1" }
                }
            })
            
            -- Test out of bounds operations
            local success = pcall(function()
                list:insert_item(5, { title = "Invalid", filter_value = "invalid" })
            end)
            assert(not success, "should fail on out of bounds insert")
            
            success = pcall(function()
                list:remove_item(5)
            end)
            assert(not success, "should fail on out of bounds remove")
            
            success = pcall(function()
                list:set_item(5, { title = "Invalid", filter_value = "invalid" })
            end)
            assert(not success, "should fail on out of bounds set")
            
            -- Test negative indices
            success = pcall(function()
                list:insert_item(-1, { title = "Invalid", filter_value = "invalid" })
            end)
            assert(not success, "should fail on negative insert index")
        `, "test_invalid_indices")

		require.NoError(t, err)
	})

	// Test batch operations
	t.Run("batch item operations", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20
            })
            
            -- Test batch insert
            local items = {}
            for i = 1, 100 do
                table.insert(items, {
                    title = "Item " .. i,
                    filter_value = "item" .. i
                })
            end
            
            list:set_items(items)
            assert(#list:items() == 100, "should handle large batch insert")
            
            -- Test batch update
            local updated_items = {}
            for i = 1, 50 do
                table.insert(updated_items, {
                    title = "Updated " .. i,
                    filter_value = "updated" .. i
                })
            end
            
            list:set_items(updated_items)
            assert(#list:items() == 50, "should handle batch update")
            assert(list:items()[1].title == "Updated 1", "should update item content")
        `, "test_batch_operations")

		require.NoError(t, err)
	})
}

// Test status message handling and lifecycle
func TestListStatusMessages(t *testing.T) {
	ctx := context.Background()

	t.Run("status message operations", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            -- Spawn list with default status message lifetime
            local list = btea.list({
                width = 40,
                height = 20,
                title = "Test List"
            })
            
            -- Test new status message
            local cmd = list:new_status_message("Test status")
            assert(cmd ~= nil, "should return command when creating status message")
            
            -- Test view after status message
            local view = list:view()
            assert(type(view) == "string", "should render view with status message")
            
            -- Spawn list with custom status message lifetime
            list = btea.list({
                width = 40,
                height = 20,
                status_message_lifetime = 2 -- 2 seconds
            })
            
            -- Test status message with custom lifetime
            cmd = list:new_status_message("Custom lifetime message")
            assert(cmd ~= nil, "should return command with custom lifetime")
            
            -- Test multiple status messages
            cmd = list:new_status_message("First message")
            local cmd2 = list:new_status_message("Second message")
            assert(cmd ~= nil and cmd2 ~= nil, "should handle multiple status messages")
            
            -- Test empty status message
            cmd = list:new_status_message("")
            assert(cmd ~= nil, "should handle empty status message")
        `, "test_status_messages")

		require.NoError(t, err)
	})

	// Test status message interaction with other features
	t.Run("status message interactions", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20,
                show_status_bar = true
            })
            
            -- Test status message with hidden status bar
            list:set_show_status_bar(false)
            local cmd = list:new_status_message("Hidden status bar")
            assert(cmd ~= nil, "should return command even with hidden status bar")
            
            -- Test status message during filtering
            list:set_show_status_bar(true)
            list:set_filtering_enabled(true)
            cmd = list:new_status_message("Filtering status")
            assert(cmd ~= nil, "should handle status message during filtering")
            
            -- Test status message with custom style
            list = btea.list({
                width = 40,
                height = 20,
                styles = {
                    status_bar = btea.style():bold():foreground("blue")
                }
            })
            cmd = list:new_status_message("Styled status")
            assert(cmd ~= nil, "should handle styled status message")
        `, "test_status_message_interactions")

		require.NoError(t, err)
	})
}

func TestListSpinner(t *testing.T) {
	ctx := context.Background()

	t.Run("basic spinner operations", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20,
                title = "Test List"
            })
            
            -- Test start spinner
            local cmd = list:start_spinner()
            assert(cmd ~= nil, "should return command when starting spinner")
            
            -- Test stop spinner
            list:stop_spinner()
            
            -- Test toggle spinner
            cmd = list:toggle_spinner()
            assert(cmd ~= nil, "should return command when toggling spinner")
            
            -- Test double toggle
            cmd = list:toggle_spinner()
            assert(cmd ~= nil, "should return command when toggling again")
            
            -- Test view with active spinner
            cmd = list:start_spinner()
            local view = list:view()
            assert(type(view) == "string", "should render view with active spinner")
        `, "test_basic_spinner")

		require.NoError(t, err)
	})

	t.Run("spinner configuration", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            -- Test spinner with custom configuration
            local list = btea.list({
                width = 40,
                height = 20,
                spinner = {
                    type = "dot",
                    style = btea.style():foreground("blue")
                }
            })
            
            local cmd = list:start_spinner()
            assert(cmd ~= nil, "should handle custom spinner configuration")
            
            -- Test spinner with minimal configuration
            list = btea.list({
                width = 40,
                height = 20,
                spinner = {
                    type = "line"
                }
            })
            
            cmd = list:start_spinner()
            assert(cmd ~= nil, "should handle minimal spinner configuration")
            
            -- Test spinner with only style
            list = btea.list({
                width = 40,
                height = 20,
                spinner = {
                    style = btea.style():foreground("green")
                }
            })
            
            cmd = list:start_spinner()
            assert(cmd ~= nil, "should handle spinner with only style")
        `, "test_spinner_config")

		require.NoError(t, err)
	})

	t.Run("spinner interaction with other features", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20,
                show_title = true,
                spinner = {
                    type = "dot",
                    style = btea.style():foreground("blue")
                }
            })
            
            -- Test spinner with hidden title
            list:set_show_title(false)
            local cmd = list:start_spinner()
            assert(cmd ~= nil, "should handle spinner with hidden title")
            
            -- Test spinner during filtering
            list:set_filtering_enabled(true)
            cmd = list:toggle_spinner()
            assert(cmd ~= nil, "should handle spinner during filtering")
            
            -- Test spinner with status message
            cmd = list:start_spinner()
            local msg_cmd = list:new_status_message("Status with spinner")
            assert(cmd ~= nil and msg_cmd ~= nil, "should handle spinner with status message")
            
            -- Test spinner with item operations
            list:set_items({
                { title = "Item 1", filter_value = "1" }
            })
            cmd = list:toggle_spinner()
            assert(cmd ~= nil, "should handle spinner during item operations")
        `, "test_spinner_interactions")

		require.NoError(t, err)
	})

	t.Run("spinner edge cases", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20
            })
            
            -- Test multiple start calls
            local cmd1 = list:start_spinner()
            local cmd2 = list:start_spinner()
            assert(cmd1 ~= nil and cmd2 ~= nil, "should handle multiple start calls")
            
            -- Test stop when not started
            list:stop_spinner()
            list:stop_spinner() -- Double stop
            
            -- Test toggle sequence
            cmd1 = list:toggle_spinner()
            cmd2 = list:toggle_spinner()
            local cmd3 = list:toggle_spinner()
            assert(cmd1 ~= nil and cmd2 ~= nil and cmd3 ~= nil, 
                   "should handle multiple toggles")
        `, "test_spinner_edge_cases")

		require.NoError(t, err)
	})
}

func TestListDelegate(t *testing.T) {
	ctx := context.Background()

	t.Run("basic delegate operations", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20,
                delegate = {
                    height = function() return 1 end,
                    spacing = function() return 0 end,
                    render = function(self, list_model, index, item)
                        -- item is the raw table passed to set_items
                        if not item then return "" end
                        return string.format("%s (%d)", item.title or "", index)
                    end
                }
            })

            -- Set items first
            list:set_items({
                {
                    title = "Test Item",
                    filter_value = "test"
                }
            })
            
            local view = list:view()
            assert(type(view) == "string", "should render with delegate")
        `, "delegate_test")
		require.NoError(t, err)
	})

	t.Run("delegate styling", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            local list = btea.list({
                width = 40,
                height = 20,
                delegate = {
                    height = function() return 1 end,
                    spacing = function() return 0 end,
                    render = function(self, list_model, index, item)
                        if not item then return "" end
                        local cursor = list_model:cursor()
                        local style = btea.style()
                        if index == cursor then
                            style = style:foreground("blue"):bold()
                        end
                        return style:render(item.title or "")
                    end
                }
            })

            list:set_items({
                { title = "Item 1", filter_value = "1" },
                { title = "Item 2", filter_value = "2" }
            })
            
            local view = list:view()
            assert(type(view) == "string", "should render with styled delegate")
            
            -- Test cursor movement
            list:cursor_down()
            view = list:view()
            assert(type(view) == "string", "should update styling with cursor")
        `, "delegate_test")
		require.NoError(t, err)
	})

	t.Run("delegate update handling", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            -- Keep click count in closure
            local click_count = 0
            
            local list = btea.list({
                width = 40,
                height = 20,
                delegate = {
                    height = function() return 1 end,
                    spacing = function() return 0 end,
                    render = function(self, list_model, index, item)
                        if not item then return "" end
                        return string.format("Clicks: %d - %s", click_count, item.title or "")
                    end,
                    update = function(self, msg, list_model)
                        if msg.type == "mouse" and msg.mouse and msg.mouse.action == "press" then
                            click_count = click_count + 1
                        end
                        return nil
                    end
                }
            })

            list:set_items({
                { title = "Clickable", filter_value = "click" }
            })
            
            -- Simulate click
            local msg = {
                type = "mouse",
                mouse = {
                    action = "press",
                    button = "left",
                    x = 1,
                    y = 1
                }
            }
            list:update(msg)
            
            local view = list:view()
            assert(type(view) == "string", "should render after update")
        `, "delegate_test")
		require.NoError(t, err)
	})

	t.Run("delegate edge cases", func(t *testing.T) {
		vm := setupListTest(t)
		defer vm.Close()

		err := vm.DoString(ctx, `
            local btea = require("btea")
            
            -- Test missing required methods
            local success = pcall(function()
                btea.list({
                    width = 40,
                    height = 20,
                    delegate = {
                        -- Missing height, spacing, render
                    }
                })
            end)
            assert(not success, "should fail with missing required methods")
            
            -- Test invalid height/spacing returns
            local list = btea.list({
                width = 40,
                height = 20,
                delegate = {
                    height = function() return -1 end, -- Invalid height
                    spacing = function() return -1 end, -- Invalid spacing
                    render = function(self, model, index, item)
                        return ""
                    end
                }
            })
            
            -- Test render with nil item
            local view = list:view()
            assert(type(view) == "string", "should handle invalid height/spacing")
            
            -- Test delegate with empty items
            list:set_items({})
            view = list:view()
            assert(type(view) == "string", "should handle empty items")
        `, "test_delegate_edge_cases")

		require.NoError(t, err)
	})
}

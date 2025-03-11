# Bubble Tea List Component in Lua

## Overview

The List component provides a scrollable, filterable list of items with keyboard navigation, search functionality, and
customizable styling. It's designed for interactive terminal applications and supports both mouse and keyboard input.

## List Creation

A list is created using the `btea.list` constructor:

```lua
local list = btea.list {
    width = 80,                    -- Width in characters (required)
    height = 24,                   -- Height in characters (required)
    title = "My List",            -- Title text (optional)
    infinite_scrolling = false,    -- Enable infinite scrolling (optional)
    show_title = true,            -- Show the title bar (optional)
    show_filter = true,           -- Show the filter input (optional)
    show_status_bar = true,       -- Show the status bar (optional)
    show_pagination = true,       -- Show pagination dots (optional)
    show_help = true,             -- Show help text (optional)
    filtering_enabled = true,      -- Enable filtering (optional)
    item_name = "item",           -- Singular name for items (optional)
    item_name_plural = "items",   -- Plural name for items (optional)
    status_message_lifetime = 1,   -- Duration in seconds for status messages (optional)
    items = {},                   -- Initial items (optional)
}
```

## Items

Items in the list must implement the following interface:

```lua
local item = {
    -- Required
    filter_value = function(self)
        return "searchable text"  -- Text used for filtering
    end,
    
    -- Optional
    title = function(self)
        return "Item Title"
    end,
    
    description = function(self)
        return "Item Description"
    end
}
```

These can be provided either as:

1. Lua tables with the above functions
2. Userdata objects implementing the required methods

## Methods

### Core Methods

```lua
-- Update list state (returns cmd or nil)
local cmd = list:update(msg)

-- Get current view
local str = list:view()
```

### Item Management

```lua
-- Get all items
local items = list:items()

-- Set all items
local cmd = list:set_items(items_table)

-- Set item at index
local cmd = list:set_item(index, item)

-- Insert item at index
local cmd = list:insert_item(index, item)

-- Remove item at index
list:remove_item(index)

-- Get selected item
local item = list:selected_item()

-- Get highlight matches for item at index
local matches = list:matches_for_item(index)
```

### Navigation

```lua
-- Get cursor position
local pos = list:cursor()

-- Move cursor
list:cursor_up()
list:cursor_down()

-- Page navigation
list:prev_page()
list:next_page()

-- Select specific index
list:select(index)

-- Reset selection
list:reset_selected()
```

### Filtering

```lua
-- Enable/disable filtering
list:set_filtering_enabled(true|false)
local enabled = list:filtering_enabled()

-- Get filter state
local state = list:filter_state()
local value = list:filter_value()
local setting = list:setting_filter()
local filtered = list:is_filtered()

-- Reset filter
list:reset_filter()
```

### Display Control

```lua
-- Set dimensions
list:set_width(width)
list:set_height(height)

-- Get dimensions
local w = list:width()
local h = list:height()

-- Toggle visibility
list:set_show_title(true|false)
list:set_show_filter(true|false)
list:set_show_status_bar(true|false)
list:set_show_pagination(true|false)
list:set_show_help(true|false)

-- Get visibility states
local show = list:show_title()
local show = list:show_filter()
local show = list:show_status_bar()
local show = list:show_pagination()
local show = list:show_help()

-- Configure status bar
list:set_status_bar_item_name(singular, plural)

-- Disable quit shortcuts
list:disable_quit_keybindings()
```

### Spinner Control

```lua
-- Control loading spinner
local cmd = list:start_spinner()
list:stop_spinner()
local cmd = list:toggle_spinner()
```

### Status Messages

```lua
-- Show status message
local cmd = list:new_status_message("Message text")
```

## Styling

The list supports extensive styling through the `styles` configuration option. Available style elements include:

```lua
styles = {
    title_bar = style,                  -- Title bar style
    title = style,                      -- Title text style
    spinner = style,                    -- Loading spinner style
    filter_prompt = style,              -- Filter input prompt style
    filter_cursor = style,              -- Filter input cursor style
    status_bar = style,                 -- Status bar style
    status_empty = style,               -- Empty status style
    status_bar_active_filter = style,   -- Active filter indicator style
    status_bar_filter_count = style,    -- Filter match count style
    no_items = style,                   -- Empty list message style
    pagination = style,                 -- Pagination style
    help = style,                       -- Help text style
    active_pagination_dot = style,      -- Active page indicator style
    inactive_pagination_dot = style,    -- Inactive page indicator style
    arabic_pagination = style,          -- Numeric pagination style
    divider_dot = style,                -- Divider style
}
```

## Keyboard Control

The list supports customizable key bindings through the `keys` configuration option:

```lua
keys = {
    cursor_up = binding,              -- Move selection up
    cursor_down = binding,            -- Move selection down
    prev_page = binding,              -- Previous page
    next_page = binding,              -- Next page
    go_to_start = binding,            -- Go to first item
    go_to_end = binding,              -- Go to last item
    filter = binding,                 -- Start filtering
    clear_filter = binding,           -- Clear current filter
    cancel_while_filtering = binding, -- Cancel filter input
    accept_while_filtering = binding, -- Accept filter input
    show_full_help = binding,         -- Show help view
    close_full_help = binding,        -- Close help view
    quit = binding,                   -- Normal quit
    force_quit = binding,             -- Force quit
}
```

## Custom Delegates

The list supports custom item rendering through a delegate interface:

```lua
delegate = {
    -- Required
    height = function(self)
        return 1  -- Height in rows for each item
    end,
    
    spacing = function(self)
        return 1  -- Spacing between items
    end,
    
    render = function(self, model, index, item)
        return "rendered item"  -- String representation of item
    end,
    
    -- Optional
    update = function(self, msg, model)
        return cmd  -- Handle updates, return command if needed
    end,
    
    short_help = function(self)
        return {binding1, binding2}  -- Return array of key bindings
    end,
    
    full_help = function(self)
        return {{binding1, binding2}}  -- Return array of binding groups
    end
}
```

## Example Usage

### Basic List

```lua
local list = btea.list {
    width = 80,
    height = 24,
    title = "Todo List",
    items = {
        { title = "Task 1", filter_value = "task 1" },
        { title = "Task 2", filter_value = "task 2" },
    }
}

function update(msg)
    local cmd = list:update(msg)
    if cmd then
        return model, cmd
    end
    return model
end

function view()
    return list:view()
end
```

### Custom Styled List

```lua
local list = btea.list {
    width = 80,
    height = 24,
    styles = {
        title = btea.style():bold():foreground("#89B4FA"),
        title_bar = btea.style():background("#1E1E2E"),
        filter_prompt = btea.style():italic():foreground("#94E2D5")
    },
    delegate = {
        height = function() return 2 end,
        spacing = function() return 1 end,
        render = function(self, model, index, item)
            local style = btea.style():padding(0, 1)
            return style:render(item.title)
        end
    }
}
```

## Important Notes

1. The list uses zero-based indexing for consistency with Go implementation
2. All style objects should be created using btea.style()
3. Key bindings should be created using btea.bind
4. Commands returned from update() must be handled by the application
5. Filter functions operate on the filter_value() results from items
6. Status messages automatically expire after the configured lifetime
7. The spinner can be used to indicate background operations
8. Custom delegates must implement all required methods

## Best Practices

1. **Item Management**
    - Keep items lightweight
    - Implement filter_value() efficiently
    - Use appropriate data structures for large lists

2. **Performance**
    - Handle update commands promptly
    - Use appropriate list heights for viewport
    - Consider filtering performance with large datasets

3. **User Experience**
    - Provide clear status messages
    - Use consistent key bindings
    - Implement helpful delegates
    - Show loading states with spinner

4. **Styling**
    - Use consistent color schemes
    - Consider terminal capabilities
    - Test different viewport sizes
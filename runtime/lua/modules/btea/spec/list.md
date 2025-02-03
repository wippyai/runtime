# Bubble Tea List Component in Lua

## Overview

The list component provides a flexible, paginated list interface with optional filtering, styling, and keyboard navigation support.

## Basic Usage

Create a new list using the `btea.new_list` constructor:

```lua
local list = btea.new_list({
    items = items,          -- Array of items implementing Item interface
    delegate = delegate,    -- ItemDelegate for rendering (optional)
    width = 80,            -- Display width
    height = 40,           -- Display height
    
    -- Optional configuration
    show_title = true,           -- Show title bar
    show_filter = true,          -- Show filter input
    show_status_bar = true,      -- Show status bar
    show_pagination = true,      -- Show pagination
    show_help = true,           -- Show help menu
    filtering_enabled = true,    -- Enable filtering
    infinite_scrolling = false,  -- Enable wrap-around scrolling
    
    -- Status bar customization
    item_name = {
        singular = "item",
        plural = "items"
    },
    
    -- Styling
    styles = {
        -- Title bar
        title_bar = btea.new_style():padding(0, 0, 1, 2),
        title = btea.new_style():background("#3E3E"),
        
        -- Filter
        filter_prompt = btea.new_style():foreground("#04B575"),
        filter_cursor = btea.new_style():foreground("#EE6FF8"),
        
        -- Status
        status_bar = btea.new_style():foreground("#A49FA5"):padding(0, 0, 1, 2),
        status_empty = btea.new_style():foreground("#909090"),
        status_bar_active_filter = btea.new_style():foreground("#1A1A1A"),
        status_bar_filter_count = btea.new_style():foreground("#DDDADA"),
        
        -- Pagination
        pagination = btea.new_style():padding_left(2),
        active_pagination_dot = btea.new_style():foreground("#847A85"),
        inactive_pagination_dot = btea.new_style():foreground("#DDDADA"),
        arabic_pagination = btea.new_style():foreground("#9B9B9B"),
        
        -- Help
        help = btea.new_style():padding(1, 0, 0, 2),
        
        -- Other
        no_items = btea.new_style():foreground("#909090"),
        divider_dot = btea.new_style():foreground("#DDDADA")
    },
    
    -- Key bindings
    keys = {
        cursor_up = btea.new_binding({
            keys = {"up", "k"},
            help = {key = "↑/k", desc = "up"}
        }),
        cursor_down = btea.new_binding({
            keys = {"down", "j"},
            help = {key = "↓/j", desc = "down"}
        }),
        prev_page = btea.new_binding({
            keys = {"left", "h", "pgup", "b", "u"},
            help = {key = "←/h/pgup", desc = "prev page"}
        }),
        next_page = btea.new_binding({
            keys = {"right", "l", "pgdown", "f", "d"},
            help = {key = "→/l/pgdn", desc = "next page"}
        }),
        go_to_start = btea.new_binding({
            keys = {"home", "g"},
            help = {key = "g/home", desc = "go to start"}
        }),
        go_to_end = btea.new_binding({
            keys = {"end", "G"},
            help = {key = "G/end", desc = "go to end"}
        }),
        filter = btea.new_binding({
            keys = {"/"},
            help = {key = "/", desc = "filter"}
        }),
        clear_filter = btea.new_binding({
            keys = {"esc"},
            help = {key = "esc", desc = "clear filter"}
        }),
        accept_filter = btea.new_binding({
            keys = {"enter", "tab", "shift+tab", "ctrl+k", "up", "ctrl+j", "down"},
            help = {key = "enter", desc = "apply filter"}
        }),
        quit = btea.new_binding({
            keys = {"q", "esc"},
            help = {key = "q", desc = "quit"}
        })
    }
})
```

## Item Interface

Items in the list must implement the Item interface:

```lua
local Item = {
    -- Return string value used for filtering
    filter_value = function(self)
        return "string to filter against"
    end
}

-- Optional: implement DefaultItem interface for use with default delegate
local DefaultItem = {
    -- Inherit Item interface
    filter_value = Item.filter_value,
    
    -- Return item title
    title = function(self)
        return "Item Title"
    end,
    
    -- Return item description
    description = function(self)
        return "Item description"
    end
}
```

## Item Delegate

The delegate controls how items are rendered. Use the default delegate or implement a custom one:

```lua
local DefaultDelegate = {
    -- Item height in rows
    height = function(self)
        return self.show_description and 2 or 1
    end,
    
    -- Vertical spacing between items
    spacing = function(self)
        return 1
    end,
    
    -- Render an item
    render = function(self, writer, model, index, item)
        local title = item:title()
        local desc = item:description()
        
        -- Apply appropriate styling based on selection/filter state
        -- Write to provided writer
    end,
    
    -- Optional: handle updates
    update = function(self, msg, model)
        -- Return command if needed
    end,
    
    -- Optional: provide additional help entries
    short_help = function(self)
        return {}
    end,
    
    full_help = function(self)
        return {}
    end
}
```

## Methods

### Item Management

```lua
-- Get/set items
local items = list:items()
list:set_items(new_items)

-- Modify specific items
list:set_item(index, item)
list:insert_item(index, item)
list:remove_item(index)

-- Get selected item
local selected = list:selected_item()
```

### Navigation

```lua
-- Move cursor
list:cursor_up()
list:cursor_down()

-- Change pages
list:prev_page()
list:next_page()

-- Jump to position
list:select(index)
list:reset_selected()  -- Go to first item
```

### Filtering

```lua
-- Get filter state
local state = list:filter_state()    -- "unfiltered", "filtering", or "filter_applied"
local value = list:filter_value()    -- Current filter text

-- Check states
local setting = list:setting_filter()  -- Currently editing filter
local filtered = list:is_filtered()    -- Filter is applied

-- Reset filtering
list:reset_filter()
```

### Display

```lua
-- Set dimensions
list:set_width(width)
list:set_height(height)

-- Show/hide elements
list:set_show_title(true|false)
list:set_show_filter(true|false)
list:set_show_status_bar(true|false)
list:set_show_pagination(true|false)
list:set_show_help(true|false)

-- Configure status bar
list:set_status_bar_item_name("thing", "things")
```

### Spinner

```lua
-- Control loading spinner
local cmd = list:start_spinner()
list:stop_spinner()
local cmd = list:toggle_spinner()
```

### Status Messages

```lua
-- Show temporary status message
local cmd = list:new_status_message("Processing...")
```

## Example Usage

### Basic List

```lua
-- Create items
local items = {
    {
        filter_value = function(self) return self.title end,
        title = function(self) return "Item 1" end,
        description = function(self) return "First item" end
    },
    -- Additional items...
}

-- Create list
local list = btea.new_list({
    items = items,
    width = 80,
    height = 40
})

-- Update function
local function update(msg)
    if msg.type == "window_size" then
        list:set_width(msg.width)
        list:set_height(msg.height)
    end
    
    return list:update(msg)
end

-- View function
local function view()
    return list:view()
end
```

### Custom Delegate

```lua
local delegate = {
    height = function(self)
        return 1
    end,
    
    spacing = function(self)
        return 0
    end,
    
    render = function(self, writer, model, index, item)
        local selected = index == model:index()
        local style = selected and "reverse" or "normal"
        writer:write(style(item:title()))
    end
}

local list = btea.new_list({
    items = items,
    delegate = delegate,
    width = 80,
    height = 40
})
```

## Notes

- Filtering is enabled by default but can be disabled
- Status messages automatically expire after 1 second
- The spinner is hidden by default
- Key bindings can be disabled via `disable_quit_keybindings()`
- Additional short/full help keys can be added via delegate
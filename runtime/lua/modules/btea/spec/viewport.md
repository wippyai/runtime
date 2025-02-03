# Bubble Tea Viewport in Lua

## Overview

This specification defines how viewport components are represented and used in Lua within the Bubble Tea framework. The viewport provides a scrollable view for content that's larger than the available screen space, with support for both keyboard and mouse-based scrolling.

## Viewport Creation

A viewport is created using the `btea.new_viewport` constructor:

```lua
local viewport = btea.new_viewport {
    width = 40,                     -- Required: viewport width
    height = 20,                    -- Required: viewport height
    content = "Multi-line text...", -- Optional: initial content
    mouse_wheel_enabled = true,     -- Optional: enable mouse wheel
    mouse_wheel_delta = 3,          -- Optional: lines per scroll
    high_performance = false,       -- Optional: performance mode
    style = btea.new_style()        -- Optional: viewport styling
        :border("rounded")
        :padding(1),
}
```

## Methods

### Content Management

```lua
-- Set viewport content
viewport:set_content("New content...")

-- Get content information
local total = viewport:total_lines()     -- Total number of lines
local visible = viewport:visible_lines() -- Currently visible lines
```

### Scrolling Control

```lua
-- Basic scrolling
viewport:line_up([n])       -- Scroll up n lines (default: 1)
viewport:line_down([n])     -- Scroll down n lines (default: 1)
viewport:page_up()          -- Scroll up one page
viewport:page_down()        -- Scroll down one page
viewport:half_page_up()     -- Scroll up half page
viewport:half_page_down()   -- Scroll down half page

-- Direct positioning
viewport:scroll_to_top()    -- Scroll to the beginning
viewport:scroll_to_bottom() -- Scroll to the end
viewport:set_y_offset(n)    -- Set precise scroll position
```

### Position Information

```lua
-- Get current position
local offset = viewport:y_offset()        -- Current scroll position
local percent = viewport:scroll_percent() -- Scroll percentage (0-1)

-- Check position
local at_top = viewport:at_top()         -- True if at the beginning
local at_bottom = viewport:at_bottom()    -- True if at the end
```

### Configuration

```lua
-- Adjust dimensions
viewport:set_width(width)   -- Set viewport width
viewport:set_height(height) -- Set viewport height

-- Mouse control
viewport:enable_mouse(true|false)        -- Enable/disable mouse
viewport:mouse_wheel_delta(lines)        -- Lines per wheel event

-- Styling
viewport:set_style(style)   -- Set viewport style
```

### Core Functions

```lua
-- Update viewport state
local cmd = viewport:update(msg)

-- Render viewport
local str = viewport:view()
```

## Best Practices

1. **Content Management**
    - Update content only when necessary
    - Consider content width when setting viewport dimensions
    - Handle multi-byte characters properly

2. **Performance**
    - Use high performance mode for large content
    - Update dimensions on window resize
    - Cache rendered content when possible

3. **User Experience**
    - Provide clear scroll indicators
    - Maintain consistent scroll behavior
    - Handle both keyboard and mouse input

4. **Styling**
    - Use borders to indicate scrollable area
    - Style scroll indicators appropriately
    - Consider terminal color support

## Example Usage

### Basic Viewport

```lua
local function create_viewport()
    return btea.new_viewport {
        width = 40,
        height = 20,
        style = btea.new_style()
            :border("rounded")
            :padding(1),
        mouse_wheel_enabled = true
    }
end

local function update(model, msg)
    if msg.type == "update" then
        -- Handle viewport updates
        local cmd = model.viewport:update(msg)
        if cmd then
            return model, cmd
        end
    end
    return model
end

local function view(model)
    return model.viewport:view()
end
```

### Log Viewer Pattern

```lua
local log_viewer = {
    viewport = btea.new_viewport {
        width = 80,
        height = 24,
        high_performance = true,
        style = btea.new_style()
            :border("rounded")
            :padding(0, 1)
    },
    
    -- Add new log entry
    append = function(self, entry)
        local current = self.viewport:content()
        self.viewport:set_content(current .. entry .. "\n")
        if self:at_bottom() then
            self.viewport:scroll_to_bottom()
        end
    end,
    
    -- Check if viewing latest entries
    at_bottom = function(self)
        return self.viewport:at_bottom()
    end,
    
    -- Auto-scroll to new entries
    auto_scroll = true
}
```

### Search Results View

```lua
local results_view = {
    viewport = btea.new_viewport {
        width = 60,
        height = 20,
        mouse_wheel_enabled = true,
        style = btea.new_style()
            :border("rounded")
            :padding(1)
    },
    
    -- Update search results
    set_results = function(self, results)
        local content = {}
        for i, result in ipairs(results) do
            table.insert(content, string.format(
                "%d. %s", i, result
            ))
        end
        self.viewport:set_content(
            table.concat(content, "\n")
        )
        self.viewport:scroll_to_top()
    end
}
```

## Notes

- Content updates immediately reflect in the viewport
- Mouse wheel support requires terminal mouse support
- High performance mode bypasses standard rendering
- Style changes affect the entire viewport area
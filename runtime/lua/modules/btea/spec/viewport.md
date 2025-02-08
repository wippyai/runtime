# Bubble Tea Viewport in Lua

## Overview

This specification defines how viewport components are represented and used in Lua within the Bubble Tea framework. The
viewport provides a scrollable view for content that's larger than the available screen space, with support for both
keyboard and mouse-based scrolling.

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
    style = some_style             -- Optional: viewport styling (must be a btea style object)
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
viewport:line_up(n)       -- Scroll up n lines (n is required)
viewport:line_down(n)     -- Scroll down n lines (n is required)
viewport:page_up()        -- Scroll up one page
viewport:page_down()      -- Scroll down one page
viewport:half_page_up()   -- Scroll up half page
viewport:half_page_down() -- Scroll down half page

-- Direct positioning
viewport:scroll_to_top()    -- Scroll to the beginning
viewport:scroll_to_bottom() -- Scroll to the end
viewport:set_y_offset(n)    -- Set precise scroll position
```

### Position Information

```lua
-- Get current position
local offset = viewport:y_offset()        -- Current scroll position
local percent = viewport:scroll_percent() -- Scroll percentage (0-100)

-- Check position
local at_top = viewport:at_top()       -- True if at the beginning
local at_bottom = viewport:at_bottom()  -- True if at the end
```

### Configuration

```lua
-- Adjust dimensions
viewport:set_width(width)   -- Set viewport width
viewport:set_height(height) -- Set viewport height

-- Mouse control
viewport:enable_mouse(true|false)        -- Enable/disable mouse wheel
viewport:mouse_wheel_delta(lines)        -- Set lines per wheel event

-- Styling
viewport:set_style(style)   -- Set viewport style (must be a btea style object)
```

### Core Functions

```lua
-- Get dimensions
local w = viewport:width()  -- Get current width
local h = viewport:height() -- Get current height

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
    - When using line_up/line_down, always provide the number of lines
    - Handle both keyboard and mouse input properly
    - Consider enabling mouse wheel for easier navigation

4. **Styling**
    - Only use valid btea style objects for styling
    - Apply styles thoughtfully to maintain readability
    - Consider terminal color support

## Example Usage

### Basic Viewport

```lua
local function create_viewport()
    local style = btea.new_style()
        :foreground("#FFFFFF")
        :background("#000000")
        
    return btea.new_viewport {
        width = 40,
        height = 20,
        style = style,
        mouse_wheel_enabled = true,
        mouse_wheel_delta = 3
    }
end

local function update(model, msg)
    if msg then
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
        mouse_wheel_enabled = true
    },
    
    -- Add new log entry
    append = function(self, entry)
        local current = self.viewport:view() -- Get current content
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

## Important Notes

1. The `line_up` and `line_down` methods require a line count parameter. Omitting it will result in an error.
2. Mouse wheel support requires both `mouse_wheel_enabled = true` and terminal mouse support.
3. Scroll percentage is returned as a number from 0 to 100, not 0 to 1.
4. Style objects must be valid btea style objects created with `btea.new_style()`.
5. The viewport's high performance mode is recommended for large content or frequent updates.
6. When setting content, the viewport maintains its scroll position unless explicitly changed.
7. Mouse wheel events automatically respect the configured `mouse_wheel_delta` value.
# Bubble Tea Progress Component in Lua

## Overview

This specification defines how progress bar components are represented and used in Lua within the Bubble Tea framework. The progress component provides an animated progress bar with support for percentage-based tracking, gradients, and customizable styling.

## Progress Creation

A progress bar is created using the `btea.progress` constructor:

```lua
local progress = btea.progress {
    width = 40,                    -- Optional: progress bar width
    show_percentage = true,        -- Optional: show percentage text (default true)
    fill_type = "gradient",        -- Optional: "gradient" or "solid"
    gradient = {                   -- Optional: custom gradient colors
        from = "#FF0000",         -- Start color
        to = "#00FF00"           -- End color
    },
    color = "#0000FF"             -- Optional: color for solid fill type
}
```

## Methods

### Core Methods

```lua
-- Update progress bar state
local cmd = progress:update(msg)

-- Render progress bar
local str = progress:view()

-- Render progress bar with specific percentage
local str = progress:view_as(0.75) -- Shows 75%
```

### Progress Control

```lua
-- Set exact progress percentage (0.0 to 1.0)
local cmd = progress:set_percent(0.5)    -- Set to 50%

-- Increment progress
local cmd = progress:incr_percent(0.1)   -- Increase by 10%

-- Decrement progress
local cmd = progress:decr_percent(0.1)   -- Decrease by 10%

-- Get current progress
local current = progress:percent()        -- Returns value between 0 and 1
```

### Configuration

```lua
-- Set progress bar width
progress:set_width(width)

-- Check animation state
local is_active = progress:is_animating()
```

## Animation and Behavior

1. The progress bar uses spring physics for smooth animations
2. Default spring configuration: tension = 30, friction = 2
3. Animations occur when:
    - Progress percentage changes
    - Width changes
    - Gradient or color updates

## Best Practices

1. **Progress Updates**
    - Use `set_percent` for absolute positions
    - Use `incr_percent`/`decr_percent` for relative changes
    - Values are automatically clamped between 0.0 and 1.0

2. **Performance**
    - Handle animation frame updates properly
    - Process update commands returned by modification methods
    - Consider width in relation to terminal size

3. **User Experience**
    - Use gradients for visual interest in longer operations
    - Show percentage for precise progress indication
    - Consider terminal color support when choosing colors

## Example Usage

### Basic Progress Bar

```lua
local function create_progress()
    return btea.progress {
        width = 40,
        show_percentage = true
    }
end

local function update(model, msg)
    if msg then
        -- Handle progress updates
        local cmd = model.progress:update(msg)
        if cmd then
            return model, cmd
        end
    end
    return model
end

local function view(model)
    return model.progress:view()
end
```

### Download Progress Example

```lua
local download_tracker = {
    progress = btea.progress {
        width = 60,
        fill_type = "gradient",
        gradient = {
            from = "#5A56E0",
            to = "#EE6FF8"
        }
    },
    
    -- Update progress based on bytes
    update_bytes = function(self, received, total)
        local percent = received / total
        return self.progress:set_percent(percent)
    end,
    
    -- Reset progress
    reset = function(self)
        return self.progress:set_percent(0)
    end
}
```

### Operation Progress with Phases

```lua
local operation = {
    progress = btea.progress {
        width = 40,
        fill_type = "solid",
        color = "#00FF00"
    },
    
    phases = {
        "Initializing",
        "Processing",
        "Finalizing"
    },
    current_phase = 1,
    
    -- Advance to next phase
    next_phase = function(self)
        self.current_phase = self.current_phase + 1
        local phase_progress = (self.current_phase - 1) / #self.phases
        return self.progress:set_percent(phase_progress)
    end
}
```

## Important Notes

1. Progress values are always normalized between 0.0 and 1.0
2. Animation commands must be handled by the application's update loop
3. The component uses Bubble Tea's frame messages for animation
4. Progress bars respect terminal color support
5. Width defaults to terminal width if not specified
6. Percentage display can be disabled for cleaner visuals
7. Custom gradients require valid color strings (hex format)
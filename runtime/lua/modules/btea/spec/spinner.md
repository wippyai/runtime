# Bubble Tea Spinner in Lua

## Overview

The spinner component provides an animated loading indicator that can be styled and customized. It supports multiple animation types and configurable update intervals.

## Spinner Creation

Create a spinner using the `btea.spinner` constructor:

```lua
local spinner = btea.spinner {
    type = btea.spinners.LINE,     -- Animation type (optional, defaults to LINE)
    interval = "100ms"             -- Update interval (optional)
}
```

### Interval Format
The interval can be specified in several formats:
- Duration string: "100ms", "1s", "500ms"
- Numeric milliseconds: 100, 500
- Must be greater than 0

## Spinner Types

Available via `btea.spinners`:

```lua
btea.spinners.LINE       -- Simple line rotation (|/-\)
btea.spinners.DOT        -- Braille dot animation
btea.spinners.MINIDOT    -- Smaller dot animation
btea.spinners.JUMP       -- Jumping dot animation
btea.spinners.PULSE      -- Pulsing block animation
btea.spinners.POINTS     -- Moving dots
btea.spinners.GLOBE      -- Rotating earth animation
btea.spinners.MOON       -- Moon phases animation
btea.spinners.MONKEY     -- Cycling monkey animation
btea.spinners.METER      -- Progress meter animation
btea.spinners.HAMBURGER  -- Menu icon animation
btea.spinners.ELLIPSIS   -- Growing ellipsis animation
```

## Methods

### Core Functions

```lua
-- Get next animation frame
local cmd = spinner:tick()

-- Update spinner state, returns animation command if needed
local cmd = spinner:update(msg)

-- Get current spinner frame
local str = spinner:view()
```

### Configuration

```lua
-- Set spinner style
spinner:style(style)  -- style should be a btea.Style object

-- Set animation interval
spinner:set_interval("100ms")  -- accepts duration string or number
```

## Animation Control

The spinner animation is controlled through the update/tick cycle:

1. Initial animation is started with `tick()`
2. Each `update()` processes frame changes
3. The returned command should be executed to continue animation

```lua
-- Start animation
local cmd = spinner:tick()

-- In your update loop
function update(msg)
    local cmd = spinner:update(msg)
    if cmd then
        -- Execute command to continue animation
        return model, cmd
    end
    return model
end
```

## Example Usage

### Basic Loading Indicator

```lua
local model = {
    spinner = btea.spinner {
        type = btea.spinners.DOT,
        interval = "100ms"
    },
    loading = true
}

function update(msg)
    if model.loading then
        local cmd = model.spinner:update(msg)
        if cmd then
            return model, cmd
        end
    end
    return model
end

function view()
    if model.loading then
        return model.spinner:view() .. " Loading..."
    end
    return "Done!"
end

-- Start animation
return model, model.spinner:tick()
```

### Styled Spinner

```lua
local spinner = btea.spinner {
    type = btea.spinners.POINTS
}

-- Apply styling
local style = btea.style()
    :foreground("#89B4FA")
    :bold()
spinner:style(style)
```

## Best Practices

1. **Animation Control**
    - Always handle the command returned from update()
    - Start animation with tick()
    - Chain commands if needed using btea.sequence

2. **Interval Management**
    - Use appropriate intervals (100ms is typical)
    - Validate interval values (must be > 0)
    - Consider performance impact of very short intervals

3. **Error Handling**
    - Validate interval format when setting
    - Handle invalid spinner types gracefully
    - Check for nil commands in update cycle

4. **State Management**
    - Track animation state (active/inactive)
    - Clean up animations when no longer needed
    - Coordinate multiple spinners if needed

## Notes

- All spinner types are predefined and cannot be modified at runtime
- Intervals affect CPU usage - use appropriate values
- Style changes affect all frames of the animation
- Animation continues until explicitly stopped or component is unmounted
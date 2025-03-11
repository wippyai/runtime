# Bubble Tea Zone Component Specification in Lua

This document defines the interface and usage patterns for the zone component in btea. Zones allow tracking and
interacting with specific regions in the terminal UI.

## Basic Concepts

Zones are regions in your terminal UI that can:

- Track their position and boundaries
- Detect mouse interactions
- Support nested and overlapping regions
- Maintain unique identifiers

## Creating and Managing Zones

Create a zone manager to track interactive regions:

```lua
local manager = btea.zone_manager()

-- Enable/disable zone tracking
manager:set_enabled(true)  -- or false

-- Check if zones are enabled
local enabled = manager:is_enabled()
```

## Core Operations

### Marking Zones

Mark content with a zone to make it interactive:

```lua
local marked = manager:mark("button-1", "Click Me!")
```

### Generating Unique IDs

To avoid ID conflicts between components:

```lua
local prefix = manager:new_prefix()

-- Use prefix when marking zones
local marked = manager:mark(prefix .. "button-1", "Click Me!")
```

### Scanning Content

The root component must scan the final view to process zone markers:

```lua
local final_view = manager:scan(view_content)
```

### Retrieving Zone Info

Get information about a marked zone:

```lua
local zone_info = manager:get("button-1")
```

## Zone Info Methods

The zone_info object provides several methods:

```lua
-- Check if a mouse event is within zone boundaries
if zone_info:in_bounds(msg.mouse) then
    -- Handle interaction
end

-- Get relative position of mouse within zone
local x, y = zone_info:pos(msg.mouse)

-- Check if zone info exists
if zone_info:is_zero() then
    -- Handle unknown zone
end
```

## Complete Example

Here's a complete example of using zones in a component:

```lua
local M = {}

function M.initial()
    return {
        manager = btea.zone_manager(),
        prefix = nil,
        items = {"Item 1", "Item 2", "Item 3"},
        selected = nil
    }
end

function M.init(model)
    model.prefix = model.manager:new_prefix()
    return model
end

function M.update(model, msg)
    if msg.mouse then
        for i, item in ipairs(model.items) do
            local zone_id = model.prefix .. "item-" .. i
            if model.manager:get(zone_id):in_bounds(msg.mouse) then
                model.selected = i
                break
            end
        end
    end
    return model
end

function M.view(model)
    local output = {}
    
    for i, item in ipairs(model.items) do
        local zone_id = model.prefix .. "item-" .. i
        local style = i == model.selected and "reverse" or "normal"
        local marked = model.manager:mark(zone_id, style(item))
        table.insert(output, marked)
    end
    
    -- Root component must scan the final output
    return model.manager:scan(table.concat(output, "\n"))
end

return M
```

## Best Practices

1. **Zone IDs**
    - Use unique prefixes for components
    - Make IDs descriptive and structured
    - Consider hierarchical naming for nested components

2. **Performance**
    - Only mark regions that need interaction
    - Clear unused zones when components unmount
    - Consider disabling zones when not needed

3. **Mouse Handling**
    - Check bounds before processing clicks
    - Use relative positioning for precise interactions
    - Consider z-index when zones overlap

4. **Integration**
    - Scan only at the root component
    - Clean up managers when closing app
    - Pass manager instance to child components that need it

## Common Patterns

### Clickable List

```lua
local function make_list(items, manager, prefix)
    local output = {}
    for i, item in ipairs(items) do
        local marked = manager:mark(prefix .. i, item)
        table.insert(output, marked)
    end
    return table.concat(output, "\n")
end
```

### Interactive Grid

```lua
local function make_grid(grid, manager, prefix)
    local output = {}
    for y, row in ipairs(grid) do
        local row_output = {}
        for x, cell in ipairs(row) do
            local id = string.format("%s_%d_%d", prefix, x, y)
            table.insert(row_output, manager:mark(id, cell))
        end
        table.insert(output, table.concat(row_output, " "))
    end
    return table.concat(output, "\n")
end
```

### Nested Components

```lua
local function parent_view(model)
    local output = {
        model.manager:mark("header", header_view()),
        model.manager:mark("sidebar", sidebar_view()),
        model.manager:mark("main", main_view())
    }
    -- Only scan at root
    return model.manager:scan(table.concat(output, "\n"))
end
```

## Notes

- Zone markers are automatically removed during scanning
- Zones persist until cleared or manager is disabled
- Mouse events only register for marked regions
- Scanning should happen only once at the root level
- Zone coordinates use 0-based indexing
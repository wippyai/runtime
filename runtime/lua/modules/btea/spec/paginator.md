# Bubble Tea Paginator in Lua

## Overview

This specification defines how paginator components are represented and used in Lua within the Bubble Tea framework. The
paginator provides functionality for handling pagination of content, including navigation and display of pagination
status.

## Important Note

The paginator uses zero-based indexing to maintain consistency with the Go implementation. This means:

- First page is 0
- Last page is (total_pages - 1)
- Navigation methods work with zero-based indices

## Paginator Creation

A paginator is created using the `btea.paginator` constructor:

```lua
local paginator = btea.paginator {
    type = btea.paginator_types.ARABIC,  -- Display type (ARABIC or DOTS)
    page = 0,                            -- Current page (0-based index)
    per_page = 10,                       -- Items per page
    total_pages = 5,                     -- Total number of pages (pages will be 0-4)
}
```

## Constants

### Display Types

```lua
btea.paginator_types.ARABIC  -- Numeric display (e.g., "1/5")
btea.paginator_types.DOTS    -- Visual indicator display
```

## Methods

### Navigation

```lua
-- Move to previous page (will not go below 0)
paginator:prev_page()

-- Move to next page (will not exceed total_pages - 1)
paginator:next_page()

-- Check position (returns true/false)
local is_first = paginator:on_first_page()  -- True if page is 0
local is_last = paginator:on_last_page()    -- True if on last available page

-- Get current page number (0-based)
local current = paginator:get_current_page()
```

### Configuration

```lua
-- Set display type
paginator:set_type(btea.paginator_types.DOTS)

-- Set total pages
paginator:set_total_pages(total)

-- Set items per page
paginator:set_per_page(amount)
```

### Page Data Helpers

```lua
-- Get number of items for current page
local items = paginator:items_on_page(total_items)

-- Get slice bounds for current page
local start_idx, end_idx = paginator:get_slice_bounds(total_length)
```

### Core Functions

```lua
-- Update paginator state (returns cmd or nil)
local cmd = paginator:update(msg)

-- Render pagination display
local str = paginator:view()
```

## Example Usage

### Basic List Pagination

```lua
-- Create model with paginator
local model = {
    items = {"item1", "item2", "item3", "item4", "item5"},
    paginator = btea.paginator {
        type = btea.paginator_types.ARABIC,
        per_page = 2
    }
}

-- Initialize total pages
model.paginator:set_total_pages(#model.items)

-- Get current page items
local function get_page_items(model)
    local start_idx, end_idx = model.paginator:get_slice_bounds(#model.items)
    local visible = {}
    
    -- Remember to adjust for Lua's 1-based array indexing
    for i = start_idx + 1, end_idx do
        table.insert(visible, model.items[i])
    end
    
    return visible
end

-- Update function
local function update(model, msg)
    local cmd = model.paginator:update(msg)
    return model, cmd
end

-- View function
local function view(model)
    local items = get_page_items(model)
    return table.concat(items, "\n") .. "\n" .. model.paginator:view()
end
```

### Dynamic Content Pagination

```lua
local search_results = {
    paginator = btea.paginator {
        type = btea.paginator_types.DOTS,
        per_page = 5
    },
    results = {},
    
    update_results = function(self, new_results)
        self.results = new_results
        self.paginator:set_total_pages(#new_results)
    end,
    
    get_page = function(self)
        local start_idx, end_idx = self.paginator:get_slice_bounds(#self.results)
        local page = {}
        for i = start_idx + 1, end_idx do
            table.insert(page, self.results[i])
        end
        return page
    end
}
```

## Best Practices

1. **Zero-Based Indexing**
    - Remember that page numbers are zero-based internally
    - Adjust array indices when accessing Lua tables (add 1 to slice bounds)
    - Use get_current_page() for logic rather than assumptions about page numbers

2. **Bounds Handling**
    - The paginator handles bounds checking internally
    - prev_page() won't go below 0
    - next_page() won't exceed (total_pages - 1)

3. **State Management**
    - Update total_pages when data changes
    - Check on_first_page() and on_last_page() for navigation logic
    - Use get_slice_bounds() for consistent page slicing

4. **Message Handling**
    - Always handle the cmd returned from update()
    - Test both keyboard and mouse navigation if supported
    - Consider update() return value for state changes

## Notes

- All page numbers are zero-based internally for consistency
- Lua table indexing still starts at 1, so adjust slice bounds accordingly
- Navigation methods handle bounds checking automatically
- Display type can be changed at runtime using set_type()
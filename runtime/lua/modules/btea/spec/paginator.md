# Bubble Tea Paginator in Lua

## Overview

This specification defines how paginator components are represented and used in Lua within the Bubble Tea framework. The
paginator provides functionality for handling pagination of content, including navigation and display of pagination
status.

## Paginator Creation

A paginator is created using the `btea.paginator` constructor:

```lua
local paginator = btea.paginator {
    type = btea.paginator_types.ARABIC,       -- Display type (ARABIC or DOTS)
    page = 0,                       -- Current page (0-based index)
    per_page = 10,                  -- Items per page
    total_pages = 5,                -- Total number of pages
    active_dot = "•",               -- Active page indicator for DOTS type
    inactive_dot = "○",             -- Inactive page indicator for DOTS type
    arabic_format = "%d/%d"         -- Format string for ARABIC type
}
```

## Constants

### Display Types

```lua
btea.paginator_types.ARABIC  -- Displays as "1/10", "2/10", etc.
btea.paginator_types.DOTS    -- Displays as "○ • ○ ○ ○" where • is current page
```

## Methods

### Navigation

```lua
-- Move to previous page if available
paginator:prev_page()

-- Move to next page if available
paginator:next_page()

-- Check position
local first = paginator:on_first_page()  -- True if on first page
local last = paginator:on_last_page()    -- True if on last page

-- Get current page number (0-based)
local current = paginator:get_current_page()
```

### Page Management

```lua
-- Set total pages based on total items
local total = paginator:set_total_pages(items)

-- Set items per page
paginator:set_per_page(amount)

-- Get number of items on current page
local items = paginator:items_on_page(total_items)

-- Get slice bounds for current page
local start, end = paginator:get_slice_bounds(total_length)
```

### Core Functions

```lua
-- Update paginator state
local cmd = paginator:update(msg)

-- Render pagination display
local str = paginator:view()
```

## Best Practices

1. **Page Numbering**
    - Pages are zero-based internally
    - Display format can show human-friendly numbers (1-based)
    - Handle edge cases (empty lists, single pages)

2. **Navigation**
    - Validate page boundaries before navigation
    - Update total pages when data changes
    - Handle keyboard navigation consistently

3. **Display**
    - Choose appropriate display type for context
    - Customize dot characters for visibility
    - Consider terminal color support

4. **Integration**
    - Sync pagination with content updates
    - Handle window resizing appropriately
    - Coordinate with other UI components

## Example Usage

### Basic List Pagination

```lua
local function create_paginated_list()
    local model = {
        items = {},  -- Your list items
        paginator = btea.paginator {
            type = btea.paginator_types.ARABIC,
            per_page = 10
        }
    }
    
    -- Initialize total pages
    model.paginator:set_total_pages(#model.items)
    
    return model
end

local function view(model)
    local start, end_ = model.paginator:get_slice_bounds(#model.items)
    local visible_items = {}
    
    for i = start + 1, end_ do
        table.insert(visible_items, model.items[i])
    end
    
    return table.concat(visible_items, "\n") .. "\n" .. model.paginator:view()
end

local function update(model, msg)
    if msg.type == "update" then
        local cmd = model.paginator:update(msg)
        if cmd then
            return model, cmd
        end
    end
    return model
end
```

### Search Results Pagination

```lua
local search_results = {
    paginator = btea.paginator {
        type = btea.paginator_types.DOTS,
        per_page = 5,
        active_dot = "●",
        inactive_dot = "○"
    },
    
    results = {},
    
    update_results = function(self, new_results)
        self.results = new_results
        self.paginator:set_total_pages(#new_results)
        self.paginator:prev_page() -- Reset to first page
    end,
    
    get_current_page_items = function(self)
        local start, end_ = self.paginator:get_slice_bounds(#self.results)
        local page_items = {}
        for i = start + 1, end_ do
            table.insert(page_items, self.results[i])
        end
        return page_items
    end
}
```

### Table View Pagination

```lua
local table_view = {
    paginator = btea.paginator {
        type = btea.paginator_types.ARABIC,
        per_page = 15,
        arabic_format = "Page %d of %d"
    },
    
    data = {},
    
    render = function(self)
        local start, end_ = self.paginator:get_slice_bounds(#self.data)
        local visible_rows = {}
        
        for i = start + 1, end_ do
            table.insert(visible_rows, self:format_row(self.data[i]))
        end
        
        return table.concat(visible_rows, "\n") .. 
               "\n" .. self.paginator:view()
    end
}
```

## Notes

- The paginator is zero-based internally
- Navigation methods handle bounds checking
- Display type can be changed at runtime
- Both DOTS and ARABIC types support customization
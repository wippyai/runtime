# Bubble Tea Help in Lua

## Overview

The help component provides a consistent way to display keyboard shortcuts and command documentation in Bubble Tea
applications. It supports both a concise single-line view for basic commands and a detailed multi-column view for
comprehensive help information.

## Help Creation

A help component is created using the `btea.help` constructor:

```lua
local help = btea.help({
    width = 80,                    -- Optional: display width
    show_all = false,             -- Optional: show full help by default
    short_separator = " • ",       -- Optional: separator for short help
    full_separator = "    ",       -- Optional: separator for full help
    ellipsis = "...",             -- Optional: truncation indicator
    styles = {                     -- Optional: custom styles
        short_key = btea.style():foreground("#909090"),
        short_desc = btea.new_style():foreground("#B2B2B2"),
        short_separator = btea.new_style():foreground("#DDDADA"),
        full_key = btea.new_style():foreground("#909090"),
        full_desc = btea.new_style():foreground("#B2B2B2"),
        full_separator = btea.new_style():foreground("#DDDADA"),
        ellipsis = btea.new_style():foreground("#DDDADA"),
    }
})
```

## Methods

### Display Control

```lua
-- Toggle between short and full help
help:set_show_all(true|false)

-- Set display width
help:set_width(width)

-- Set separators
help:set_separators(short_sep, full_sep)

-- Set truncation indicator
help:set_ellipsis(ellipsis)
```

### Styling

```lua
-- Set all styles at once
help:set_styles({
    short_key = style,       -- Style for keys in short help
    short_desc = style,      -- Style for descriptions in short help
    short_separator = style, -- Style for separator in short help
    full_key = style,       -- Style for keys in full help
    full_desc = style,      -- Style for descriptions in full help
    full_separator = style, -- Style for separator in full help
    ellipsis = style,      -- Style for truncation indicator
})
```

### Core Functions

```lua
-- Update help state
local cmd = help:update(msg)

-- Render help view with keymap
local str = help:view(keymap)

-- Get current bindings
local short_bindings = help:get_short_help(keymap)
local full_bindings = help:get_full_help(keymap)
```

## KeyMap Interface

The help component can work with both Lua tables and Bubble Tea components that implement the KeyMap interface:

### Lua Table Implementation

```lua
local keymap = {
    -- Return array of key bindings for short help
    short_help = function()
        return {
            binding1,
            binding2,
            -- ...
        }
    end,
    
    -- Return array of binding groups for full help
    full_help = function()
        return {
            {binding1, binding2},  -- First column
            {binding3, binding4},  -- Second column
            -- ...
        }
    end
}
```

### Component Integration

Components like viewport and text_input automatically implement the KeyMap interface:

```lua
local viewport = btea.new_viewport({...})
local help_text = help:view(viewport)  -- Works directly with components
```

## Best Practices

1. **Content Organization**
    - Keep short help concise and focused
    - Group related commands in full help
    - Use consistent key descriptions
    - Limit short help to essential commands

2. **Display**
    - Set appropriate width for the terminal
    - Use clear visual separation between sections
    - Ensure good contrast with styling
    - Handle multi-byte characters properly

3. **Integration**
    - Update help on window resize
    - Toggle between views when needed
    - Cache rendered help when possible
    - Handle both component and custom keymaps

4. **Styling**
    - Use consistent colors across the application
    - Ensure readability with chosen styles
    - Consider terminal color support
    - Match application theme

## Example Usage

### Basic Help Display

```lua
local function create_help()
    return btea.help({
        width = 80,
        styles = {
            short_key = btea.new_style()
                :foreground("#909090")
                :bold(),
            short_desc = btea.new_style()
                :foreground("#B2B2B2"),
        }
    })
end

local function update(model, msg)
    if msg.type == "window_size" then
        model.help:set_width(msg.window_size.width)
    end
    return model
end

local function view(model)
    return model.help:view(model.keymap)
end
```

### Custom KeyMap Example

```lua
local app_keymap = {
    -- Define bindings
    bindings = {
        quit = btea.bind({
            keys = {"q", "ctrl+c"},
            help = {key = "q/ctrl+c", desc = "quit"}
        }),
        help = btea.bind({
            keys = {"?"},
            help = {key = "?", desc = "toggle help"}
        }),
        save = btea.bind({
            keys = {"ctrl+s"},
            help = {key = "ctrl+s", desc = "save"}
        })
    },
    
    -- Implement KeyMap interface
    short_help = function(self)
        return {
            self.bindings.quit,
            self.bindings.help
        }
    end,
    
    full_help = function(self)
        return {
            {  -- File operations
                self.bindings.save,
                self.bindings.quit
            },
            {  -- Help
                self.bindings.help
            }
        }
    end
}
```

### Help with Multiple Components

```lua
local app = {
    help = btea.help({width = 80}),
    viewport = btea.new_viewport({...}),
    input = btea.text_input({...}),
    
    view = function(self)
        local output = {
            self.viewport:view(),
            self.input:view(),
            -- Show combined help
            self.help:view({
                short_help = function()
                    -- Combine bindings from both components
                    local viewport_help = self.help:get_short_help(self.viewport)
                    local input_help = self.help:get_short_help(self.input)
                    return vim.list_extend(viewport_help, input_help)
                end,
                full_help = function()
                    return {
                        self.help:get_full_help(self.viewport)[1],  -- Viewport bindings
                        self.help:get_full_help(self.input)[1]      -- Input bindings
                    }
                end
            })
        }
        return table.concat(output, "\n")
    end
}
```

## Notes

- Help display automatically truncates to fit width
- Components implementing KeyMap can be used directly
- Style changes affect all subsequent renders
- Full help view supports multiple columns
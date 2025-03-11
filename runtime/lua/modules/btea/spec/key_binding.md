# Key Binding in Lua

## Overview

This specification defines how key bindings are represented and manipulated in Lua. Key bindings provide a way to map
keyboard input to actions and help text, with support for key combinations and modifiers.

## Key Binding Creation

Key bindings are created using the `btea.bind` constructor:

```lua
local binding = btea.bind {
    keys = {"up", "k"},           -- Single key or array of keys
    help = {
        key = "↑/k",             -- Display text for keys in help menu
        desc = "move up"         -- Description in help menu
    }
}
```

### Key Specification

Keys can be specified as strings in the following formats:

1. Single characters: `"a"`, `"b"`, `"c"`, etc.
2. Special keys: `"up"`, `"down"`, `"left"`, `"right"`, `"home"`, `"end"`, `"pgup"`, `"pgdown"`, `"tab"`, `"enter"`,
   `"esc"`
3. Control combinations: `"ctrl+c"`, `"ctrl+x"`, etc.
4. Alt combinations: `"alt+a"`, `"alt+b"`, etc.
5. Function keys: `"f1"` through `"f20"`
6. Spaces and special characters: `" "` (space), `"\\"`(backslash)

## Methods

### Enabling/Disabling

```lua
-- Enable or disable the binding
binding:set_enabled(true|false)

-- Check if binding is enabled
local is_enabled = binding:is_enabled()
```

### Help Information

```lua
-- Get help information
local help = binding:help()
-- Returns: { key = "display keys", desc = "description" }
```

### Key Matching

```lua
-- Check if a key message matches this binding
if msg.type == "key" and binding:matches(msg) then
    -- Handle matching key
end
```

## Common Key Combinations

```lua
-- Navigation
local up = btea.bind {
    keys = {"up", "k"},
    help = {key = "↑/k", desc = "up"}
}

-- Control combinations
local save = btea.bind {
    keys = {"ctrl+s"},
    help = {key = "^S", desc = "save"}
}

-- Alt combinations
local word = btea.bind {
    keys = {"alt+right", "alt+f"},
    help = {key = "M-→/f", desc = "word forward"}
}

-- Multiple alternatives
local quit = btea.bind {
    keys = {"q", "ctrl+c"}, 
    help = {key = "q/^C", desc = "quit"}
}
```

## Best Practices

1. **Consistent Help Text**
    - Use `↑` `→` `↓` `←` for arrows
    - Use `^X` for Control-X
    - Use `M-X` for Alt-X
    - Separate alternatives with `/`

2. **Key Selection**
    - Provide intuitive alternatives (e.g., both arrows and vim-style keys)
    - Use standard conventions when possible (ctrl+c for quit, etc.)
    - Consider cross-platform compatibility

3. **Help Descriptions**
    - Keep descriptions short and clear
    - Use consistent verbs (e.g., "move" vs "go")
    - Start with action verbs

## Example Usage

### Basic Binding Usage

```lua
-- Create a binding
local binding = btea.bind {
    keys = {"enter", "ctrl+m"},
    help = {key = "enter", desc = "confirm"}
}

-- Check key messages
function update(msg)
    if msg.type == "key" and binding:matches(msg) then
        -- Handle key press
        return "confirmed"
    end
end
```

### Key Groups

```lua
-- Define related bindings
local keys = {
    up = btea.bind {
        keys = {"up", "k"},
        help = {key = "↑/k", desc = "move up"}
    },
    down = btea.bind {
        keys = {"down", "j"},
        help = {key = "↓/j", desc = "move down"}
    },
    confirm = btea.bind {
        keys = {"enter"},
        help = {key = "enter", desc = "confirm"}
    },
    cancel = btea.bind {
        keys = {"esc"},
        help = {key = "esc", desc = "cancel"}
    }
}

-- Use in update function
function update(msg)
    if msg.type == "key" then
        if keys.up:matches(msg) then
            move_up()
        elseif keys.down:matches(msg) then
            move_down()
        elseif keys.confirm:matches(msg) then
            confirm_action()
        elseif keys.cancel:matches(msg) then
            cancel_action()
        end
    end
end
```

## Notes

- Key bindings are immutable once created
- Disabled bindings will not match any keys
- Help text is optional but recommended
- Keys are case-sensitive
- Order of keys in the keys array doesn't matter for matching
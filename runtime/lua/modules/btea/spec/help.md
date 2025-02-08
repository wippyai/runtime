# Bubble Tea Help Component Specification

## Constructor

### btea.help(options: table) -> Help
Creates a new help component for displaying keyboard shortcuts and command documentation.

Options table fields:
- `width` (number, optional): Display width in characters
- `show_all` (boolean, optional): Show full help by default. Default: false
- `short_separator` (string, optional): Separator for short help view. Default: " • "
- `full_separator` (string, optional): Separator for full help view. Default: "    "
- `ellipsis` (string, optional): Truncation indicator. Default: "..."
- `styles` (table, optional): Style configuration containing:
   - `short_key` (Style): Style for keys in short help
   - `short_desc` (Style): Style for descriptions in short help
   - `short_separator` (Style): Style for separator in short help
   - `full_key` (Style): Style for keys in full help
   - `full_desc` (Style): Style for descriptions in full help
   - `full_separator` (Style): Style for separator in full help
   - `ellipsis` (Style): Style for truncation indicator

## Methods

### update(msg: table) -> Command|nil
Updates the help component state based on the received message. Returns a command if state changes, nil otherwise.

Message types supported:
- `window_resize`: Updates width/height
- `key`: Handles keyboard input

### view(keymap: table|KeyMap) -> string
Renders the help component with the provided keymap. The keymap can be either:
- A Lua table implementing the KeyMap interface
- A component that implements the KeyMap interface

### set_width(width: number) -> nil
Sets the display width in characters.

### set_show_all(show: boolean) -> nil
Toggles between short and full help display.

### set_styles(styles: table) -> nil
Sets the styles for help display. The styles table should contain:
- `short_key` (Style)
- `short_desc` (Style)
- `short_separator` (Style)
- `full_key` (Style)
- `full_desc` (Style)
- `full_separator` (Style)
- `ellipsis` (Style)

### set_separators(short_sep: string, full_sep: string) -> nil
Sets the separators for both help views.
- `short_sep`: Separator for short help view
- `full_sep`: Separator for full help view (defaults to "    ")

### set_ellipsis(ellipsis: string) -> nil
Sets the truncation indicator string.

### get_short_help(keymap: table|KeyMap) -> table
Returns an array of key bindings for short help from the provided keymap.

### get_full_help(keymap: table|KeyMap) -> table
Returns an array of binding groups for full help from the provided keymap.

## KeyMap Interface

A keymap can be implemented either as a Lua table or as a component.

### Table Implementation
```lua
{
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

Both `short_help` and `full_help` can also be direct tables instead of functions:
```lua
{
    short_help = {binding1, binding2},
    full_help = {
        {binding1, binding2},
        {binding3, binding4}
    }
}
```

### Component Integration
Components like viewport and text_input that implement the KeyMap interface can be passed directly to help methods:
```lua
local viewport = btea.viewport(...)
local help_text = help:view(viewport)
```

## Binding Format

Each binding in the keymap should be created using `btea.bind()`:
```lua
btea.bind({
    keys = {"key1", "key2"},  -- Array of key combinations
    help = {
        key = "display_key",  -- How the key should be displayed in help
        desc = "description"  -- Description of what the key does
    }
})
```

## Example Usage

```lua
-- Create help component with styling
local help = btea.help({
    width = 80,
    styles = {
        short_key = btea.style():foreground("#909090"):bold(),
        short_desc = btea.style():foreground("#B2B2B2"),
        short_separator = btea.style():foreground("#DDDADA"),
    }
})

-- Create keymap
local keymap = {
    bindings = {
        quit = btea.bind({
            keys = {"q", "ctrl+c"},
            help = {key = "q/ctrl+c", desc = "quit"}
        }),
        help = btea.bind({
            keys = {"?"},
            help = {key = "?", desc = "toggle help"}
        })
    },
    
    short_help = function(self)
        return {
            self.bindings.quit,
            self.bindings.help
        }
    end,
    
    full_help = function(self)
        return {
            {self.bindings.quit},  -- First column
            {self.bindings.help}   -- Second column
        }
    end
}

-- Update component
local cmd = help:update(msg)  -- Handle update message
local view = help:view(keymap)  -- Render help text
```
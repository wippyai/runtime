# Text Input Integration Guide

## Basic Usage

The most basic usage of text input in Lua looks like this:

```lua
-- Create a new text input instance
local input = btea.text_input({
    prompt = "> ",                    -- Optional prompt prefix
    placeholder = "Enter text...",    -- Optional placeholder text
    value = "",                       -- Initial value
})

-- Focus the input to receive keyboard events
-- Returns a command if focus state changes
local cmd = input:focus()

-- Basic update function
function update(msg)
    if msg.type == "update" then
        -- Update input state and get any commands
        -- Returns a command if input state changes (e.g., suggestion accepted)
        local cmd = input:update(msg)
        if cmd then
            return cmd
        end
    end
end

-- Render the input
-- Returns a string representation of the current input state
function view()
    return input:view()
end
```

## Core Methods

```lua
-- Focus the input for keyboard events
-- @return tea.Cmd|nil Command if focus state changes
input:focus()

-- Remove focus from input
-- @return nil
input:blur()

-- Update input state based on message
-- @param msg table Tea message (key events, etc.)
-- @return tea.Cmd|nil Command if state changes
input:update(msg)

-- Get string representation of current state
-- @return string Rendered input view
input:view()

-- Reset input to initial state
-- @return nil
input:reset()

-- Get current input value
-- @return string Current text value
input:value()

-- Set input value
-- @param value string New text value
-- @return nil
input:set_value(value)
```

## Advanced Configuration

Text input supports various configuration options:

```lua
local input = btea.text_input({
    -- Basic options
    prompt = "$ ",                    -- Prompt prefix
    placeholder = "Type command...",  -- Placeholder text
    value = "",                       -- Initial value
    
    -- Styling
    prompt_style = btea.style():foreground("#00FF00"):bold(),     -- Prompt style
    text_style = btea.style():foreground("#FFFFFF"),              -- Input text style
    placeholder_style = btea.style():foreground("#666666"),       -- Placeholder text style
    completion_style = btea.style():foreground("#888888"),        -- Suggestion style
    cursor_style = btea.style():foreground("#FFFF00"),           -- Cursor style
    
    -- Input constraints
    char_limit = 100,           -- Maximum character limit (nil for no limit)
    width = 40,                 -- Display width (horizontal scroll if content exceeds)
    
    -- Input mode
    echo_mode = "password",     -- "normal", "password", or "none"
    echo_character = "*",       -- Character to show in password mode
    blink_speed = "500ms",      -- Cursor blink interval (as duration string)
    
    -- Validation
    validate = function(value)
        if #value < 3 then
            return "Input must be at least 3 characters"
        end
        return nil  -- Return nil for valid input
    end,
    
    -- Autocomplete
    show_suggestions = true,    -- Enable/disable suggestions
    suggestions = {             -- List of suggestion strings
        "help",
        "status",
        "quit",
        "clear",
    }
})
```

## Configuration Methods

```lua
-- Set new placeholder text
-- @param text string New placeholder
-- @return nil
input:set_placeholder(text)

-- Set new prompt text
-- @param text string New prompt
-- @return nil
input:set_prompt(text)

-- Set character limit
-- @param limit number|nil Maximum characters (nil for no limit)
-- @return nil
input:set_char_limit(limit)

-- Set display width
-- @param width number Maximum display width
-- @return nil
input:set_width(width)

-- Update component style
-- @param type string "prompt", "text", "placeholder", "completion", or "cursor"
-- @param style Style New style to apply
-- @return nil
input:set_style(type, style)

-- Set new validation function
-- @param fn function(value: string) -> string|nil
-- @return nil
input:set_validate(fn)

-- Set suggestion list
-- @param suggestions table List of suggestion strings
-- @return nil
input:set_suggestions(suggestions)

-- Get current suggestions
-- @return table List of current suggestions
input:get_suggestions()
```

## Cursor Control Methods

```lua
-- Get current cursor position
-- @return number Zero-based cursor position
input:position()

-- Set cursor position
-- @param pos number New cursor position (clamped to text bounds)
-- @return nil
input:set_cursor(pos)

-- Move cursor to start
-- @return nil
input:cursor_start()

-- Move cursor to end
-- @return nil
input:cursor_end()
```

## Validation Methods

```lua
-- Check if current value is valid
-- @return boolean True if valid
input:is_valid()

-- Get current validation error if any
-- @return string|nil Error message or nil if valid
input:error()
```

## Key Bindings

Text input comes with default key bindings that can be customized:

```lua
-- Create custom key bindings
local bindings = {
    -- Navigation
    character_forward = btea.bind({
        keys = {"right", "ctrl+f"},
        help = {key = "→/^F", desc = "move forward"}
    }),
    character_backward = btea.bind({
        keys = {"left", "ctrl+b"},
        help = {key = "←/^B", desc = "move backward"}
    }),
    
    -- Word navigation
    word_forward = btea.bind({
        keys = {"alt+right", "alt+f"},
        help = {key = "M-→/M-F", desc = "word forward"}
    }),
    word_backward = btea.bind({
        keys = {"alt+left", "alt+b"},
        help = {key = "M-←/M-B", desc = "word backward"}
    }),
    
    -- Deletion
    delete_character_backward = btea.bind({
        keys = {"backspace", "ctrl+h"},
        help = {key = "⌫/^H", desc = "delete backward"}
    }),
    delete_character_forward = btea.bind({
        keys = {"delete", "ctrl+d"},
        help = {key = "⌦/^D", desc = "delete forward"}
    }),
    delete_word_backward = btea.bind({
        keys = {"alt+backspace", "alt+h"},
        help = {key = "M-⌫/M-H", desc = "delete word backward"}
    }),
    delete_word_forward = btea.bind({
        keys = {"alt+delete", "alt+d"},
        help = {key = "M-⌦/M-D", desc = "delete word forward"}
    }),
    delete_before_cursor = btea.bind({
        keys = {"ctrl+u"},
        help = {key = "^U", desc = "delete to start"}
    }),
    delete_after_cursor = btea.bind({
        keys = {"ctrl+k"},
        help = {key = "^K", desc = "delete to end"}
    }),
    
    -- Line navigation
    line_start = btea.bind({
        keys = {"home", "ctrl+a"},
        help = {key = "⇱/^A", desc = "line start"}
    }),
    line_end = btea.bind({
        keys = {"end", "ctrl+e"},
        help = {key = "⇲/^E", desc = "line end"}
    }),
    
    -- Clipboard
    paste = btea.bind({
        keys = {"ctrl+v", "ctrl+y"},
        help = {key = "^V/^Y", desc = "paste"}
    }),
    
    -- Completion
    accept_suggestion = btea.bind({
        keys = {"tab"},
        help = {key = "⇥", desc = "complete"}
    }),
    next_suggestion = btea.bind({
        keys = {"down", "ctrl+n"},
        help = {key = "↓/^N", desc = "next suggestion"}
    }),
    prev_suggestion = btea.bind({
        keys = {"up", "ctrl+p"},
        help = {key = "↑/^P", desc = "previous suggestion"}
    })
}
```

## State Behavior

1. Focus State
   - Methods that modify input only work when focused
   - Unfocused input still displays but doesn't process input
   - Focus/blur triggers command for state updates

2. Cursor Behavior
   - Cursor position clamped to text bounds
   - Word navigation stops at word boundaries
   - Selection not supported in current version

3. Validation States
   - Validation runs on every text change
   - Invalid state shows error but allows continued input
   - Error cleared when input becomes valid

4. Event Processing
   - Key events processed in update() when focused
   - Suggestion selection via keys generates command
   - Clipboard paste handled via paste binding

## Best Practices

1. **Error Handling**
   - Always validate input before processing
   - Show clear error messages when validation fails
   - Handle edge cases (empty input, maximum length, etc.)

2. **User Experience**
   - Use appropriate styles for different states (normal, error, disabled)
   - Provide meaningful placeholders
   - Show completion suggestions when relevant
   - Use consistent key bindings

3. **Integration**
   - Keep input state in your model
   - Handle special keys appropriately
   - Process commands returned from update()
   - Clean up resources when done (blur input)

4. **Styling**
   - Use consistent colors and styles
   - Make sure error states are visible
   - Style placeholder text appropriately
   - Consider terminal color support

5. **Performance**
   - Avoid expensive validation on every keystroke
   - Consider debouncing rapid input
   - Be mindful of suggestion list size
   - Clean up event listeners when removing input

## Common Patterns

### Form Input
```lua
local form = {
    username = btea.text_input({
        prompt = "Username: ",
        validate = function(v) return #v >= 3 end
    }),
    password = btea.text_input({
        prompt = "Password: ",
        echo_mode = "password",
        validate = function(v) return #v >= 8 end
    })
}
```

### Command Input
```lua
local cmd_input = btea.text_input({
    prompt = "$ ",
    show_suggestions = true,
    suggestions = {"help", "status", "quit"},
    validate = function(cmd)
        local valid_commands = {help = true, status = true, quit = true}
        if not valid_commands[cmd] then
            return "Unknown command"
        end
        return nil
    end
})
```

### Search Input
```lua
local search = btea.text_input({
    prompt = "🔍 ",
    placeholder = "Search...",
    key_map = {
        -- Override enter to perform search
        submit = btea.bind({
            keys = {"enter"},
            help = {key = "⏎", desc = "search"}
        })
    }
})
```
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
local cmd = input:focus()

-- Basic update function
function update(msg)
    if msg.type == "update" then
        -- Update input state and get any commands
        local cmd = input:update(msg)
        if cmd then
            return cmd
        end
    end
end

-- Render the input
function view()
    return input:view()
end
```

## Advanced Configuration

Text input supports various configuration options:

```lua
local input = btea.text_input({
    -- Basic options
    prompt = "$ ",
    placeholder = "Type command...",
    value = "",
    
    -- Styling
    prompt_style = btea.new_style():foreground("#00FF00"):bold(),
    text_style = btea.new_style():foreground("#FFFFFF"),
    placeholder_style = btea.new_style():foreground("#666666"),
    
    -- Input constraints
    char_limit = 100,           -- Maximum character limit
    width = 40,                 -- Display width (horizontal scroll if content exceeds)
    
    -- Input mode
    echo_mode = "password",     -- "normal", "password", or "none"
    echo_character = "*",       -- Character to show in password mode
    
    -- Validation
    validate = function(value)
        if #value < 3 then
            return "Input must be at least 3 characters"
        end
        return nil  -- Return nil for valid input
    end,
    
    -- Autocomplete
    show_suggestions = true,
    suggestions = {
        "help",
        "status",
        "quit",
        "clear",
    }
})
```

## Key Bindings

Text input comes with default key bindings that can be customized:

```lua
-- Create custom key bindings
local bindings = {
    -- Navigation
    character_forward = btea.new_binding({
        keys = {"right", "ctrl+f"},
        help = {key = "→/^F", desc = "move forward"}
    }),
    character_backward = btea.new_binding({
        keys = {"left", "ctrl+b"},
        help = {key = "←/^B", desc = "move backward"}
    }),
    
    -- Word navigation
    word_forward = btea.new_binding({
        keys = {"alt+right", "alt+f"},
        help = {key = "M-→/M-F", desc = "word forward"}
    }),
    word_backward = btea.new_binding({
        keys = {"alt+left", "alt+b"},
        help = {key = "M-←/M-B", desc = "word backward"}
    }),
    
    -- Deletion
    delete_character_backward = btea.new_binding({
        keys = {"backspace", "ctrl+h"},
        help = {key = "⌫/^H", desc = "delete backward"}
    }),
    delete_character_forward = btea.new_binding({
        keys = {"delete", "ctrl+d"},
        help = {key = "⌦/^D", desc = "delete forward"}
    }),
    
    -- Line manipulation
    delete_line = btea.new_binding({
        keys = {"ctrl+u"},
        help = {key = "^U", desc = "clear line"}
    }),
    
    -- Completion
    accept_suggestion = btea.new_binding({
        keys = {"tab"},
        help = {key = "⇥", desc = "complete"}
    }),
    next_suggestion = btea.new_binding({
        keys = {"down", "ctrl+n"},
        help = {key = "↓/^N", desc = "next suggestion"}
    }),
    prev_suggestion = btea.new_binding({
        keys = {"up", "ctrl+p"},
        help = {key = "↑/^P", desc = "previous suggestion"}
    })
}

-- Create input with custom bindings
local input = btea.text_input({
    key_map = bindings
})
```

## Integration Example

Here's a complete example showing how to integrate text input into a Bubble Tea application:

```lua
local M = {}

M.initial = function()
    return {
        input = btea.text_input({
            prompt = "> ",
            placeholder = "Type command...",
            show_suggestions = true,
            suggestions = {"help", "status", "quit"}
        }),
        messages = {},  -- Store input history
    }
end

M.update = function(model, msg)
    if msg.type == "update" then
        if msg.key then
            -- Handle special keys
            if msg.key.key_type == "enter" then
                -- Process completed input
                table.insert(model.messages, model.input:value())
                -- Reset input
                model.input:set_value("")
                return model
            end
        end
        
        -- Update input state
        local cmd = model.input:update(msg)
        if cmd then
            return model, cmd
        end
    end
    return model
end

M.view = function(model)
    local output = {}
    
    -- Show message history
    for _, msg in ipairs(model.messages) do
        table.insert(output, msg)
    end
    
    -- Show current input
    table.insert(output, model.input:view())
    
    return table.concat(output, "\n")
end

return M
```

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
        submit = btea.new_binding({
            keys = {"enter"},
            help = {key = "⏎", desc = "search"}
        })
    }
})
```
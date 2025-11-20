# Lua Btea Module Specification

## Overview

The `btea` module provides a Bubble Tea-inspired terminal UI framework for building interactive command-line applications in Lua.

## Documentation

Detailed documentation is available in the `spec/` directory:

- [cmd.md](spec/cmd.md) - Command execution and external processes
- [help.md](spec/help.md) - Help text and documentation generation
- [key_binding.md](spec/key_binding.md) - Keyboard input handling
- [list.md](spec/list.md) - List component for item selection
- [msg.md](spec/msg.md) - Message passing and updates
- [progress.md](spec/progress.md) - Progress bars and indicators
- [render_util.md](spec/render_util.md) - Rendering utilities
- [spinner.md](spec/spinner.md) - Loading spinners and animations
- [style.md](spec/style.md) - Text styling and colors
- [viewport.md](spec/viewport.md) - Viewport and scrolling
- [zone.md](spec/zone.md) - Zone management for layout

## Quick Example

```lua
local btea = require("btea")

-- Define initial model
local model = {
  counter = 0
}

-- Initialize function
local function init()
  return model
end

-- Update function handles messages
local function update(msg, model)
  if msg.type == "key" then
    if msg.key == "q" then
      return model, btea.quit()
    elseif msg.key == "up" then
      model.counter = model.counter + 1
    elseif msg.key == "down" then
      model.counter = model.counter - 1
    end
  end
  return model
end

-- View function renders the UI
local function view(model)
  return string.format("Counter: %d\nPress up/down to change, q to quit", model.counter)
end

-- Run the application
btea.run(init, update, view)
```

For complete API documentation and advanced examples, see the files in the `spec/` directory.

## Architecture

Btea follows the Elm Architecture pattern:
- **Model**: Application state
- **Init**: Initialize the model
- **Update**: Handle messages and update the model
- **View**: Render the model to a string

## Notes

- Based on the Bubble Tea framework design
- Provides components for common UI patterns
- Supports keyboard input, styling, and layout
- Ideal for interactive CLI tools and TUIs

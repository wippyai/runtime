# Bubble Tea Spinner in Lua

## Overview

The spinner component provides an animated loading indicator with various styles and customization options.

## Creating a Spinner

Spinners are created using the `btea.new_spinner` constructor:

```lua
local spinner = btea.new_spinner {
    type = btea.spinners.DOT  -- Optional, defaults to LINE
}
```

## Available Spinner Types

The following spinner types are provided via `btea.spinners`:

- `LINE`: Simple line rotation (`|`, `/`, `-`, `\`)
- `DOT`: Braille dot animation
- `MINIDOT`: Smaller dot animation
- `JUMP`: Jumping dot animation
- `PULSE`: Pulsing block animation
- `POINTS`: Moving dots
- `GLOBE`: Rotating earth emoji (🌍 🌎 🌏)
- `MOON`: Moon phases (🌑 🌒 🌓 🌔 🌕 🌖 🌗 🌘)
- `MONKEY`: Cycling monkey emojis (🙈 🙉 🙊)
- `METER`: Progress meter animation
- `HAMBURGER`: Menu icon animation
- `ELLIPSIS`: Growing ellipsis (...) animation

## Methods

### Update

```lua
local cmd = spinner:update(msg)
```

Updates the spinner state based on input messages. Returns a command if animation should continue.

### View

```lua
local str = spinner:view()
```

Returns the current spinner frame as a string.

### Style

```lua
spinner:style(style)
```

Sets the style for the spinner using a Lip Gloss style object.

## Example Usage

Basic usage:

```lua
local spinner = btea.new_spinner {
    type = btea.spinners.DOT
}

-- Style the spinner
local style = btea.new_style()
    :foreground("#89B4FA")
    :bold()
    
spinner:style(style)

-- Update and view in your app loop
function update(msg)
    local cmd = spinner:update(msg)
    if cmd then
        -- Handle animation command
    end
end

function view()
    return spinner:view() .. " Loading..."
end
```
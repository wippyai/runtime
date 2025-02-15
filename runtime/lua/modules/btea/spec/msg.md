# Bubble Tea Message Types in Lua

## Overview

This specification defines how Bubble Tea messages are represented in Lua after conversion from Go. Each message is converted into a Lua table with a specific structure based on its type.

## Message Structure

All messages are represented as Lua tables with a common base structure:

```lua
{
  type = "update",  -- Common base type for all messages
  -- Additional type-specific fields follow
}
```

## Key Messages

Key messages represent keyboard input events.

```lua
{
  type = "update",
  key = {
    type = "key",
    key_type = string,  -- See Key Types section
    alt = boolean,      -- Alt modifier state
    paste = boolean,    -- Paste mode flag
    string = string,    -- String representation
    runes = string,     -- Only present if key_type is "runes"
  }
}
```

### Key Types

The following key types are supported (string values):

1. Control Keys:
   - `"ctrl+@"` through `"ctrl+z"`
   - `"ctrl+\\"`, `"ctrl+]"`, `"ctrl+^"`, `"ctrl+_"`

2. Navigation Keys:
   - `"up"`, `"down"`, `"right"`, `"left"`
   - `"home"`, `"end"`
   - `"pgup"`, `"pgdown"`
   - `"tab"`, `"backspace"`, `"delete"`
   - `"insert"`, `"space"`, `"enter"`, `"esc"`
   - `"runes"` (for regular character input)

3. Shifted Variants:
   - `"shift+tab"`, `"shift+up"`, `"shift+down"`
   - `"shift+left"`, `"shift+right"`
   - `"shift+home"`, `"shift+end"`

4. Ctrl Variants:
   - `"ctrl+up"`, `"ctrl+down"`, `"ctrl+right"`, `"ctrl+left"`
   - `"ctrl+home"`, `"ctrl+end"`
   - `"ctrl+pgup"`, `"ctrl+pgdown"`

5. Ctrl+Shift Variants:
   - `"ctrl+shift+up"`, `"ctrl+shift+down"`
   - `"ctrl+shift+left"`, `"ctrl+shift+right"`
   - `"ctrl+shift+home"`, `"ctrl+shift+end"`

6. Function Keys:
   - `"f1"` through `"f20"`

## Mouse Messages

Mouse messages represent mouse input events.

```lua
{
  type = "update",
  mouse = {
    type = "mouse",
    x = number,        -- X coordinate
    y = number,        -- Y coordinate
    button = string,   -- See Mouse Buttons section
    action = string,   -- "press", "release", or "motion"
    alt = boolean,     -- Alt modifier state
    ctrl = boolean,    -- Ctrl modifier state
    shift = boolean,   -- Shift modifier state
  }
}
```

### Mouse Buttons

The following mouse button types are supported (string values):
- `"none"`: No button (motion events)
- `"left"`: Left mouse button
- `"middle"`: Middle mouse button
- `"right"`: Right mouse button
- `"wheel_up"`: Mouse wheel scroll up
- `"wheel_down"`: Mouse wheel scroll down
- `"wheel_left"`: Mouse wheel scroll left
- `"wheel_right"`: Mouse wheel scroll right
- `"backward"`: Browser back button
- `"forward"`: Browser forward button
- `"button10"`: Extended mouse button 10
- `"button11"`: Extended mouse button 11

## Window Size Messages

Window size messages represent terminal window dimension changes.

```lua
{
  type = "update",
  window_size = {
    type = "window_size",
    width = number,    -- Window width in columns
    height = number,   -- Window height in rows
  }
}
```

## Opaque Messages

For custom message types not covered above, messages are converted to an opaque format:

```lua
{
  type = "update",
  opaque = userdata,     -- Original Go message stored as userdata
  string = string,       -- String representation of message
}
```

## Error Handling

1. Unknown key types will return an error: "unknown key type: {type}"
2. Unknown mouse buttons will return an error: "unknown mouse button: {button}"

## Best Practices

1. Always check the message type before accessing type-specific fields
2. Handle unknown key types and mouse buttons gracefully
3. For opaque messages, use the string representation for display
4. Remember that alt/ctrl/shift modifiers are booleans
5. Use the string field for display/logging purposes when available
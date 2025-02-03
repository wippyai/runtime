Below is the complete TEXT AREA spec in Lua with snake_case variable names and **all** key bindings detailed:

---

# Bubble Tea Text Area Specification in Lua

This document defines the interface and usage patterns for the text_area component in btea. The text_area is a multiline
input widget that supports a variety of configuration options and methods to control behavior and appearance.

---

## Creating a Text Area

Instantiate a new text_area using the `btea.new_text_area` Lua constructor. The constructor accepts a table of options:

```lua
local text_area = btea.new_text_area({
  prompt = "> ",                      -- Optional prompt displayed before user input
  placeholder = "type something...",  -- Placeholder text when empty
  value = "",                         -- Initial content
  width = 50,                         -- Display width
  height = 10,                        -- Display height
  char_limit = 200,                   -- Maximum characters allowed
  show_line_numbers = true,           -- Display line numbers along the side
  focused_style = {                   -- Styling for focused state (table or btea.Style userdata)
      base = my_base_style,
      cursor_line = my_cursor_line_style,
      cursor_line_number = my_cursor_line_number_style,
      end_of_buffer = my_end_buffer_style,
      line_number = my_line_number_style,
      placeholder = my_placeholder_style,
      prompt = my_prompt_style,
      text = my_text_style,
  },
  blurred_style = {                   -- Styling when not focused
      base = my_base_style,
      cursor_line = my_cursor_line_style,
      cursor_line_number = my_cursor_line_number_style,
      end_of_buffer = my_end_buffer_style,
      line_number = my_line_number_style,
      placeholder = my_placeholder_style,
      prompt = my_prompt_style,
      text = my_text_style,
  },
  key_map = {                         -- Custom key bindings (all available bindings are listed below)
      character_forward         = btea.new_binding({ keys = {"right", "ctrl+f"} }),
      character_backward        = btea.new_binding({ keys = {"left", "ctrl+b"} }),
      word_forward              = btea.new_binding({ keys = {"alt+right", "alt+f"} }),
      word_backward             = btea.new_binding({ keys = {"alt+left", "alt+b"} }),
      delete_character_backward = btea.new_binding({ keys = {"backspace", "ctrl+h"} }),
      delete_character_forward  = btea.new_binding({ keys = {"delete", "ctrl+d"} }),
      insert_newline            = btea.new_binding({ keys = {"enter"} }),
      -- Additional bindings can be added here if needed.
  }
})
```

---

## Methods

The text_area component provides the following methods (all using snake_case):

### update

Updates the text_area state based on an incoming message (typically from the Bubble Tea update loop). It returns a
command (if any) to be executed.

```lua
local command = text_area:update(msg)
if command then
  -- Execute the command as needed.
end
```

### view

Renders the current state of the text_area to a string for display:

```lua
local view_str = text_area:view()
```

### set_value

Directly sets or updates the text_area's value:

```lua
text_area:set_value("new text content")
```

### value

Retrieves the current content of the text_area:

```lua
local current_value = text_area:value()
```

### focus and blur

Manages the focus state to start or stop capturing keyboard events:

```lua
-- To focus (this may return a command to be executed)
local focus_command = text_area:focus()
if focus_command then
  -- Execute the focus command as required.
end

-- To blur (remove focus from the text_area)
text_area:blur()
```

---

## Message Handling

In your update loop, pass incoming messages (e.g., key events) to the text_area:

```lua
function update(msg)
  local command = text_area:update(msg)
  if command then
    return command  -- Return or process the command accordingly
  end
  -- Continue with additional update logic...
end
```

---

## Styling

Styles for the text_area are configured via the `focused_style` and `blurred_style` options. Each style should provide
the following fields (each field is expected to be a btea.Style userdata):

- `base`
- `cursor_line`
- `cursor_line_number`
- `end_of_buffer`
- `line_number`
- `placeholder`
- `prompt`
- `text`

Define your style values (using snake_case variable names) and pass them as part of the configuration when constructing
the text_area.

---

## Custom Key Bindings

The text_area supports several key bindings. These bindings can be overridden by supplying a `key_map` table with custom
bindings. The full list of available key bindings is as follows:

- **character_forward**:  
  Moves the cursor one character forward.  
  *Default keys*: `"right"`, `"ctrl+f"`

- **character_backward**:  
  Moves the cursor one character backward.  
  *Default keys*: `"left"`, `"ctrl+b"`

- **word_forward**:  
  Moves the cursor forward by one word.  
  *Default keys*: `"alt+right"`, `"alt+f"`

- **word_backward**:  
  Moves the cursor backward by one word.  
  *Default keys*: `"alt+left"`, `"alt+b"`

- **delete_character_backward**:  
  Deletes the character behind the cursor.  
  *Default keys*: `"backspace"`, `"ctrl+h"`

- **delete_character_forward**:  
  Deletes the character ahead of the cursor.  
  *Default keys*: `"delete"`, `"ctrl+d"`

- **insert_newline**:  
  Inserts a newline at the current cursor position (useful for multi-line input).  
  *Default key*: `"enter"`

These bindings can be fully customized as shown in the creation example above.

---

## Example Usage

Below is a complete example showing how to integrate and use the text_area component:

```lua
-- Define your styles (assumed to be valid btea.Style userdata)
local my_base_style            = btea.new_style():foreground("#FFFFFF")
local my_cursor_line_style     = btea.new_style():background("#333333")
local my_cursor_line_number_style = btea.new_style():foreground("#00FF00")
local my_end_buffer_style      = btea.new_style():foreground("#666666")
local my_line_number_style     = btea.new_style():foreground("#999999")
local my_placeholder_style     = btea.new_style():italic(true)
local my_prompt_style          = btea.new_style():bold(true)
local my_text_style            = btea.new_style()

-- Create a text_area with basic options and full key bindings.
local text_area = btea.new_text_area({
  prompt = "> ",
  placeholder = "type your message...",
  width = 60,
  height = 5,
  char_limit = 250,
  show_line_numbers = true,
  focused_style = {
      base = my_base_style,
      cursor_line = my_cursor_line_style,
      cursor_line_number = my_cursor_line_number_style,
      end_of_buffer = my_end_buffer_style,
      line_number = my_line_number_style,
      placeholder = my_placeholder_style,
      prompt = my_prompt_style,
      text = my_text_style,
  },
  blurred_style = {
      base = my_base_style,
      cursor_line = my_cursor_line_style,
      cursor_line_number = my_cursor_line_number_style,
      end_of_buffer = my_end_buffer_style,
      line_number = my_line_number_style,
      placeholder = my_placeholder_style,
      prompt = my_prompt_style,
      text = my_text_style,
  },
  key_map = {
      character_forward         = btea.new_binding({ keys = {"right", "ctrl+f"} }),
      character_backward        = btea.new_binding({ keys = {"left", "ctrl+b"} }),
      word_forward              = btea.new_binding({ keys = {"alt+right", "alt+f"} }),
      word_backward             = btea.new_binding({ keys = {"alt+left", "alt+b"} }),
      delete_character_backward = btea.new_binding({ keys = {"backspace", "ctrl+h"} }),
      delete_character_forward  = btea.new_binding({ keys = {"delete", "ctrl+d"} }),
      insert_newline            = btea.new_binding({ keys = {"enter"} }),
  }
})

-- Example update loop:
function update(msg)
  local command = text_area:update(msg)
  if command then
    -- Process the returned command, if any.
    return command
  end
  -- Additional update logic can be added here.
end

-- Example view/render function:
function view()
  return text_area:view()
end
```

---

## Notes

- **Command Integration:** The `update`, `focus`, and other methods may return Bubble Tea commands which must be
  executed within your application loop.
- **Styling Conversion:** Lua tables provided for styles are internally converted into lipgloss.Style values. Ensure
  your style definitions meet the expected format.
- **Key Bindings:** Custom key bindings should be supplied as btea.Binding userdata objects. The complete set of
  available bindings is shown above.

This specification provides a comprehensive overview of the text_area component, including all key bindings and methods,
using snake_case for variables and function names.
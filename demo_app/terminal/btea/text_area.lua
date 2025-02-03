function App()
  local inbox = tasks.channel()
  local done = channel.new()
  local cmd_channel = channel.new(128)

  -- Set these values based on your terminal dimensions.
  -- For a full-width layout, you might dynamically calculate these.
  local full_width = 100   -- Example: full terminal width
  local full_height = 30   -- Example: height for the text area

  -- Create a full-width text_area with Norton Commander styling.
  local text_area = btea.new_text_area({
    prompt = "NC> ",
    placeholder = "Enter commands or text here...",
    value = "",
    width = full_width,
    height = full_height,
    char_limit = 2000,
    show_line_numbers = true,
    focused_style = {
      base = btea.new_style()
          :border("double")
          :padding(1)
          :background("#000080")  -- deep navy blue
          :foreground("#FFFFFF"),
      cursor_line = btea.new_style():background("#0000A0"),
      cursor_line_number = btea.new_style():foreground("#FFFF00"),
      end_of_buffer = btea.new_style():foreground("#AAAAAA"),
      line_number = btea.new_style():foreground("#FFFFFF"),
      placeholder = btea.new_style():italic(true):foreground("#CCCCCC"),
      prompt = btea.new_style():bold(true):foreground("#FFFF00"),
      text = btea.new_style():foreground("#FFFFFF")
    },
    blurred_style = {
      base = btea.new_style()
          :border("double")
          :padding(1)
          :background("#000080")
          :foreground("#FFFFFF"),
      cursor_line = btea.new_style():background("#000060"),
      cursor_line_number = btea.new_style():foreground("#FFFF00"),
      end_of_buffer = btea.new_style():foreground("#AAAAAA"),
      line_number = btea.new_style():foreground("#FFFFFF"),
      placeholder = btea.new_style():italic(true):foreground("#CCCCCC"),
      prompt = btea.new_style():bold(true):foreground("#FFFF00"),
      text = btea.new_style():foreground("#FFFFFF")
    },
    key_map = {
      character_forward         = btea.new_binding({ keys = {"right", "ctrl+f"} }),
      character_backward        = btea.new_binding({ keys = {"left", "ctrl+b"} }),
      word_forward              = btea.new_binding({ keys = {"alt+right", "alt+f"} }),
      word_backward             = btea.new_binding({ keys = {"alt+left", "alt+b"} }),
      delete_character_backward = btea.new_binding({ keys = {"backspace", "ctrl+h"} }),
      delete_character_forward  = btea.new_binding({ keys = {"delete", "ctrl+d"} }),
      insert_newline            = btea.new_binding({ keys = {"enter"} })
    }
  })

  -- Focus the text_area on startup.
  text_area:focus()

  local function render_view()
    local view_lines = {}
    table.insert(view_lines, "Norton Commander - Full Width Text Area Demo")
    table.insert(view_lines, "")
    table.insert(view_lines, text_area:view())
    table.insert(view_lines, "")
    table.insert(view_lines, "Press Ctrl+Enter to submit, Esc or Ctrl+C to quit.")
    return table.concat(view_lines, "\n")
  end

  -- Send initial commands: enter alt screen, hide cursor, set window title.
  cmd_channel:send(btea.batch({
    btea.commands.enter_alt_screen,
    btea.commands.hide_cursor,
    btea.commands.set_window_title("Norton Commander Demo")
  }))

  -- Command processor coroutine.
  coroutine.spawn(function()
    while true do
      local result = channel.select({
        cmd_channel:case_receive(),
        done:case_receive()
      })
      if result.channel == done then
        break
      else
        local cmd = result.value
        if cmd then
          local msg = cmd:execute()
          if msg then upstream.send(msg) end
        end
      end
    end
  end)

  -- Main application loop.
  while true do
    local task, ok = inbox:receive()
    if not ok then
      done:send(true)
      break
    end

    local msg = task:input()

    if type(msg) == "table" then
      if msg.type == "update" then
        if msg.key then
          -- Quit if Esc or Ctrl+C is pressed.
          if msg.key.key_type == "esc" or msg.key.key_type == "ctrl+c" then
            break
          -- Submit when Ctrl+Enter is pressed.
          elseif msg.key.key_type == "enter" and msg.key.ctrl then
            print("Submitted text:")
            print(text_area:value())
            text_area:set_value("")
          end
        end

        local cmd = text_area:update(msg)
        if cmd then cmd_channel:send(cmd) end

        task:complete(render_view())
      elseif msg.type == "view" then
        task:complete(render_view())
      else
        task:complete("ok")
      end
    else
      task:complete("ok")
    end
  end

  -- Cleanup: close channels and restore terminal state.
  done:close()
  cmd_channel:send(btea.batch({
    btea.commands.show_cursor,
    btea.commands.exit_alt_screen
  }))
end

return App

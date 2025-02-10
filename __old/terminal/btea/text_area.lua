local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Create text area with Norton Commander styling
    app.text_area = btea.text_area({
        prompt = "NC> ",
        placeholder = "Enter commands or text here...",
        value = "",
        width = app.window.width,
        height = app.window.height - 4,  -- Leave room for header and footer
        char_limit = 2000,
        show_line_numbers = true,
        focused_style = {
            base = btea.style()
                :border("double")
                :padding(1)
                :background("#000080")  -- deep navy blue
                :foreground("#FFFFFF"),
            cursor_line = btea.style():background("#0000A0"),
            cursor_line_number = btea.style():foreground("#FFFF00"),
            end_of_buffer = btea.style():foreground("#AAAAAA"),
            line_number = btea.style():foreground("#FFFFFF"),
            placeholder = btea.style():italic(true):foreground("#CCCCCC"),
            prompt = btea.style():bold(true):foreground("#FFFF00"),
            text = btea.style():foreground("#FFFFFF")
        },
        blurred_style = {
            base = btea.style()
                :border("double")
                :padding(1)
                :background("#000080")
                :foreground("#FFFFFF"),
            cursor_line = btea.style():background("#000060"),
            cursor_line_number = btea.style():foreground("#FFFF00"),
            end_of_buffer = btea.style():foreground("#AAAAAA"),
            line_number = btea.style():foreground("#FFFFFF"),
            placeholder = btea.style():italic(true):foreground("#CCCCCC"),
            prompt = btea.style():bold(true):foreground("#FFFF00"),
            text = btea.style():foreground("#FFFFFF")
        },
        key_map = {
            character_forward = btea.bind({ keys = {"right", "ctrl+f"} }),
            character_backward = btea.bind({ keys = {"left", "ctrl+b"} }),
            word_forward = btea.bind({ keys = {"alt+right", "alt+f"} }),
            word_backward = btea.bind({ keys = {"alt+left", "alt+b"} }),
            delete_character_backward = btea.bind({ keys = {"backspace", "ctrl+h"} }),
            delete_character_forward = btea.bind({ keys = {"delete", "ctrl+d"} }),
            insert_newline = btea.bind({ keys = {"enter"} })
        }
    })

    -- Focus the text_area on startup
    app.text_area:focus()

    -- Setup key bindings with correct format
    app.keys = bapp.create_keys({
        quit = { keys = { "q", "ctrl+c" } },
        submit = { keys = { "ctrl+enter" }, help = { key = "^Enter", desc = "submit" } }
    })

    -- Update function
    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.submit:matches(msg) then
                print("Submitted text:")
                print(self.text_area:value())
                self.text_area:set_value("")
            end

            local cmd = self.text_area:update(msg)
            if cmd then self:dispatch(cmd) end
        end
        return false
    end

    -- View function
    local function view(self)
        local view_lines = {}
        table.insert(view_lines, "Norton Commander - Full Width Text Area Demo")
        table.insert(view_lines, "")
        table.insert(view_lines, self.text_area:view())
        table.insert(view_lines, "")
        table.insert(view_lines, "Press Ctrl+Enter to submit, Esc or Ctrl+C to quit.")
        return table.concat(view_lines, "\n")
    end

    -- Run the app
    app:run(update, view)
end

return App
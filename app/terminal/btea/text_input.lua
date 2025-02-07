local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Create different text inputs with various styles
    app.inputs = {
        {
            name = "Default",
            input = btea.new_text_input({
                prompt = "> ",
                placeholder = "Basic input...",
                width = 40
            })
        },
        {
            name = "Password",
            input = btea.new_text_input({
                prompt = "🔒 ",
                placeholder = "Enter password...",
                echo_mode = btea.ECHO_PASSWORD,
                echo_character = "•",
                width = 40
            })
        },
        {
            name = "Limited",
            input = btea.new_text_input({
                prompt = "# ",
                placeholder = "Max 10 chars...",
                char_limit = 10,
                width = 40
            })
        },
        {
            name = "With Validation",
            input = btea.new_text_input({
                prompt = "$ ",
                placeholder = "Numbers only...",
                width = 40,
                validate = function(s)
                    if s:match("^%d*$") then
                        return nil
                    end
                    return "Only numbers allowed"
                end
            })
        },
        {
            name = "With Suggestions",
            input = btea.new_text_input({
                prompt = "cmd: ",
                placeholder = "Type command...",
                width = 40,
                show_suggestions = true,
                suggestions = { "help", "status", "quit", "clear", "restart" }
            })
        }
    }

    -- Track current input and results
    app.current = 1
    app.results = {}

    -- Style definitions
    app.styles = {
        base = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E"),

        title = btea.new_style()
            :foreground("#CDD6F4")
            :bold(),

        label = btea.new_style()
            :foreground("#89B4FA"),

        result = btea.new_style()
            :foreground("#A6E3A1")
            :italic(),

        error = btea.new_style()
            :foreground("#F38BA8")
            :italic(),

        help = btea.new_style()
            :foreground("#6C7086")
            :italic()
    }

    -- Setup key bindings
    app.keys = bapp.create_keys({
        next = {
            keys = { "tab", "down" },
            help = { key = "tab/↓", desc = "next input" }
        },
        prev = {
            keys = { "shift+tab", "up" },
            help = { key = "shift+tab/↑", desc = "prev input" }
        },
        quit = {
            keys = { "ctrl+c", "esc" },
            help = { key = "^C/esc", desc = "quit" }
        }
    })

    -- Focus first input
    app.inputs[app.current].input:focus()

    -- Update function
    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.next:matches(msg) then
                -- Move to next input
                self.inputs[self.current].input:blur()
                self.current = (self.current % #self.inputs) + 1
                local cmd = self.inputs[self.current].input:focus()
                if cmd then self:dispatch(cmd) end
                return false
            elseif self.keys.prev:matches(msg) then
                -- Move to previous input
                self.inputs[self.current].input:blur()
                self.current = ((self.current - 2) % #self.inputs) + 1
                local cmd = self.inputs[self.current].input:focus()
                if cmd then self:dispatch(cmd) end
                return false
            elseif msg.key.key_type == "enter" then
                -- Store input result
                self.results[self.current] = self.inputs[self.current].input:value()
                local cmd = self.inputs[self.current].input:set_value("")
                if cmd then self:dispatch(cmd) end
                return false
            end
        end

        -- Update current input only if we haven't handled the key
        local cmd = self.inputs[self.current].input:update(msg)
        if cmd then self:dispatch(cmd) end
        return false
    end

    -- View function
    local function view(self)
        local lines = {
            self.styles.title:render("Text Input Types Demo"),
            ""
        }

        for i, input in ipairs(self.inputs) do
            -- Add input label
            table.insert(lines, self.styles.label:render(input.name))
            -- Add the input itself - view now returns string directly
            local input_view = input.input:view()
            table.insert(lines, input_view)
            -- Add result or error if any
            if self.results[i] then
                table.insert(lines, self.styles.result:render("→ " .. self.results[i]))
            end
            if not input.input:is_valid() then
                table.insert(lines, self.styles.error:render("✗ " .. input.input:error()))
            end
            table.insert(lines, "")
        end

        -- Add help text
        table.insert(lines, self.styles.help:render("Tab/↓ next | Shift+Tab/↑ prev | Enter submit | ^C/Esc quit"))

        return self.styles.base:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App

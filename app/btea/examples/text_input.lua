local bapp = require("bapp")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- Create different text inputs with various styles
    app.inputs = {
        {
            name = "Default",
            input = btea.text_input({
                prompt = "> ",
                placeholder = "Basic input...",
                width = 40
            })
        },
        {
            name = "Password",
            input = btea.text_input({
                prompt = "🔒 ",
                placeholder = "Enter password...",
                echo_mode = btea.ECHO_PASSWORD,
                echo_character = "•",
                width = 40
            })
        },
        {
            name = "Limited",
            input = btea.text_input({
                prompt = "# ",
                placeholder = "Max 10 chars...",
                char_limit = 10,
                width = 40
            })
        },
        {
            name = "With Validation",
            input = btea.text_input({
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
            input = btea.text_input({
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
        base = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E"),

        title = btea.style()
            :foreground("#CDD6F4")
            :bold(),

        label = btea.style()
            :foreground("#89B4FA"),

        result = btea.style()
            :foreground("#A6E3A1")
            :italic(),

        error = btea.style()
            :foreground("#F38BA8")
            :italic(),

        help = btea.style()
            :foreground("#6C7086")
            :italic()
    }

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "esc"},
            help = {key = "^C/esc", desc = "quit"}
        }),
        next = btea.bind({
            keys = {"tab", "down"},
            help = {key = "tab/↓", desc = "next"}
        }),
        prev = btea.bind({
            keys = {"shift+tab", "up"},
            help = {key = "↑/shift+tab", desc = "previous"}
        }),
        submit = btea.bind({
            keys = {"enter"},
            help = {key = "enter", desc = "submit"}
        })
    }

    -- Focus first input
    app:dispatch(app.inputs[app.current].input:focus())

    -- Helper functions
    local function next_input(self)
        self.inputs[self.current].input:blur()
        self.current = (self.current % #self.inputs) + 1
        local cmd = self.inputs[self.current].input:focus()
        if cmd then self:dispatch(cmd) end
    end

    local function prev_input(self)
        self.inputs[self.current].input:blur()
        self.current = ((self.current - 2) % #self.inputs) + 1
        local cmd = self.inputs[self.current].input:focus()
        if cmd then self:dispatch(cmd) end
    end

    local function submit_input(self)
        self.results[self.current] = self.inputs[self.current].input:value()
        local cmd = self.inputs[self.current].input:set_value("")
        if cmd then self:dispatch(cmd) end
    end

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            -- Update input widths based on new window size
            for _, input in ipairs(self.inputs) do
                input.input:set_width(math.min(40, self.window.width - 4))
            end
        end

        -- Handle key bindings
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            elseif self.keys.next:matches(msg) then
                next_input(self)
            elseif self.keys.prev:matches(msg) then
                prev_input(self)
            elseif self.keys.submit:matches(msg) then
                submit_input(self)
            end
        end

        -- Update current input
        local cmd = self.inputs[self.current].input:update(msg)
        if cmd then self:dispatch(cmd) end

        return false -- continue running
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
            -- Add the input itself
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

        -- Add help text with simpler formatting
        local help_lines = {
            "tab/↓ next",
            "shift+tab/↑ previous",
            "enter submit",
            "^C/esc quit"
        }
        table.insert(lines, self.styles.help:render(table.concat(help_lines, " | ")))

        return self.styles.base:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App
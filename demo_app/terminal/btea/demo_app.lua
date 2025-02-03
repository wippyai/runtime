local bapp = require("base_app")

function App()
    -- Create app instance
    local app = bapp.new()

    -- Setup state
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
                width = 40
            })
        }
    }
    local cmd = app.inputs[1].input:focus()
    if cmd then app.cmd_channel:send(cmd) end

    app.current = 1
    app.results = {}

    -- Setup key bindings
    app.keys = bapp.create_keys({
        next = {
            keys = { "tab", "down" },
            help = { key = "tab/↓", desc = "next input" }
        },
        prev = {
            keys = { "shift+tab", "up" },
            help = { key = "shift+tab/↑", desc = "prev input" }
        }
    })

    -- Update function
    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true -- quit
            elseif self.keys.next:matches(msg) then
                self.inputs[self.current].input:blur()
                self.current = (self.current % #self.inputs) + 1
                local cmd = self.inputs[self.current].input:focus()
                if cmd then self.cmd_channel:send(cmd) end
            elseif self.keys.prev:matches(msg) then
                self.inputs[self.current].input:blur()
                self.current = ((self.current - 2) % #self.inputs) + 1
                local cmd = self.inputs[self.current].input:focus()
                if cmd then self.cmd_channel:send(cmd) end
            elseif msg.key.key_type == "enter" then
                self.results[self.current] = self.inputs[self.current].input:value()
                self.inputs[self.current].input:set_value("")
            end
        end

        -- Update current input
        local cmd = self.inputs[self.current].input:update(msg)
        if cmd then self.cmd_channel:send(cmd) end
        return false -- continue
    end

    -- View function
    local function view(self)
        local lines = {
            bapp.styles.title:render("Text Input Demo"),
            ""
        }

        for i, input in ipairs(self.inputs) do
            table.insert(lines, input.name)
            table.insert(lines, input.input:view())
            if self.results[i] then
                table.insert(lines, "→ " .. self.results[i])
            end
            table.insert(lines, "")
        end

        table.insert(lines, bapp.styles.help:render(
            "Tab/↓ next | Shift+Tab/↑ prev | Enter submit | q/^C quit"
        ))

        return bapp.styles.base:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App

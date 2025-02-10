local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Create zone manager for handling mouse interactions
    local zone_manager = btea.zone_manager()

    -- Create inputs with different configurations
    app.inputs = {
        {
            name = "Name",
            input = btea.text_input({
                prompt = "👤 ",
                placeholder = "Enter your name...",
                width = 40
            }),
            zone_id = "input-name"
        },
        {
            name = "Email",
            input = btea.text_input({
                prompt = "📧 ",
                placeholder = "Enter your email...",
                width = 40
            }),
            zone_id = "input-email"
        },
        {
            name = "Password",
            input = btea.text_input({
                prompt = "🔒 ",
                placeholder = "Enter password...",
                echo_mode = btea.ECHO_PASSWORD,
                echo_character = "•",
                width = 40
            }),
            zone_id = "input-password"
        }
    }

    -- Track current focused input
    app.current = 1

    -- Style definitions
    app.styles = {
        container = btea.style()
            :border("rounded")
            :padding(1)
            :background("#1E1E2E"),

        input_normal = btea.style()
            :border("rounded")
            :padding(0, 1)
            :background("#313244"),

        input_focused = btea.style()
            :border("rounded")
            :padding(0, 1)
            :background("#45475A"),

        label = btea.style()
            :foreground("#89B4FA"),

        help = btea.style()
            :foreground("#6C7086")
            :italic()
    }

    -- Enable mouse support
    app.cmd_channel:send(btea.commands.enable_mouse_all_motion)

    -- Focus first input
    app:dispatch(app.inputs[app.current].input:focus())

    -- Update function
    local function update(self, msg)
        if msg.key and msg.key.key_type == "ctrl+c" then
            return true
        end

        -- Handle mouse events for zone detection
        if msg.mouse and msg.mouse.type == "mouse" then
            -- Use any_in_bounds_update to check and handle mouse interactions
            for i, input_data in ipairs(self.inputs) do
                if zone_manager:get(input_data.zone_id):in_bounds(msg) then
                    if msg.mouse.action == "press" and i ~= self.current then
                        -- Blur current input
                        self.inputs[self.current].input:blur()
                        -- Update current input index
                        self.current = i
                        -- Focus new input
                        local cmd = self.inputs[self.current].input:focus()
                        if cmd then self:dispatch(cmd) end
                    end
                    break
                end
            end
        end

        -- Update current input
        local cmd = self.inputs[self.current].input:update(msg)
        if cmd then self:dispatch(cmd) end
        return false
    end

    -- View function
    local function view(self)
        local lines = {}

        for i, input_data in ipairs(self.inputs) do
            -- Add input label
            table.insert(lines, self.styles.label:render(input_data.name))

            -- Style based on focus state
            local style = (i == self.current) and self.styles.input_focused or self.styles.input_normal

            -- Wrap input view with zone marker
            local input_view = style:render(input_data.input:view())
            table.insert(lines, zone_manager:mark(input_data.zone_id, input_view))
            table.insert(lines, "")
        end

        -- Add help text
        table.insert(lines, self.styles.help:render("Click to focus | ^C to quit"))

        -- Combine everything and scan for zones
        return zone_manager:scan(
            self.styles.container:render(
                table.concat(lines, "\n")
            )
        )
    end

    -- Run the app
    app:run(update, view)
end

return App
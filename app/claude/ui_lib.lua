local json = require("json")
local time = require("time")

-- UI Library for Claude TUI
local UI = {}

-- Initialize UI components and styles
function UI.new()
    local ui = {}

    -- Define styles
    ui.styles = {
        box = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1, 2)
            :foreground("#89B4FA")
            :background("#1E1E2E")
            :border_foreground("#89B4FA"),
        header = btea.style()
            :bold()
            :foreground("#CBA6F7")
            :padding(0, 1),
        user = btea.style()
            :foreground("#F5C2E7"),
        assistant = btea.style()
            :foreground("#A6E3A1"),
        system = btea.style()
            :foreground("#FAB387"),
        timestamp = btea.style()
            :foreground("#6C7086"),
        error = btea.style()
            :foreground("#F38BA8")
            :italic(),
        help = btea.style()
            :foreground("#6C7086")
            :italic(),
        tool = btea.style()
            :foreground("#89DCEB")
            :bold(),
        debug = btea.style()
            :foreground("#6C7086")
            :italic()
    }

    -- Define key bindings
    ui.keys = {
        quit = btea.bind({
            keys = { "ctrl+c", "esc" },
            help = { key = "^C/esc", desc = "quit" }
        }),
        submit = btea.bind({
            keys = { "enter" },
            help = { key = "enter", desc = "send message" }
        }),
        clear = btea.bind({
            keys = { "ctrl+l" },
            help = { key = "^L", desc = "clear chat" }
        }),
        toggle_debug = btea.bind({
            keys = { "ctrl+d" },
            help = { key = "^D", desc = "toggle debug" }
        }),
        execute_tool = btea.bind({
            keys = { "ctrl+t" },
            help = { key = "^T", desc = "execute tool" }
        })
    }

    -- Initialize components
    ui.init_components = function(self, app)
        -- Initialize text input
        app.input = btea.text_input({
            prompt = "> ",
            placeholder = "Ask Claude something...",
            width = app.window.width - 8
        })

        -- Initialize viewport for debug logs
        app.debug_view = btea.viewport({
            width = app.window.width - 8,
            height = 10,
            mouse_wheel_enabled = true
        })

        -- Focus the input immediately
        app:dispatch(app.input:focus())

        return app
    end

    -- Main view function
    ui.render = function(self, app)
        local content_width = app.window.width - 6
        local header_divider = string.rep("═", content_width)
        local content = {
            self.styles.header:render("Claude Terminal Interface"),
            self.styles.timestamp:render(header_divider)
        }

        -- Calculate visible messages
        local max_visible = app.window.height - 8 - (app.show_debug and 12 or 0)
        local start_idx = math.max(1, #app.messages - max_visible)

        -- Add messages
        for i = start_idx, #app.messages do
            local msg = app.messages[i]
            local timestamp = msg.timestamp:format("15:04:05")
            local styled_time = self.styles.timestamp:render(timestamp)
            local style = self.styles[msg.type]

            -- Format prefix
            local prefix = ""
            if msg.type == "user" then
                prefix = "You: "
            elseif msg.type == "assistant" then
                prefix = "Claude: "
            elseif msg.type == "system" then
                prefix = "System: "
            elseif msg.type == "tool" then
                prefix = "Tool: "
            elseif msg.type == "error" then
                prefix = "Error: "
            end

            local styled_text = style:render(prefix .. msg.content)
            table.insert(content, styled_time .. " " .. styled_text)
        end

        -- Add debug logs
        if app.show_debug then
            table.insert(content, "")
            table.insert(content, self.styles.debug:render("Debug log:"))
            table.insert(content, app.debug_view:view())
        end

        -- Add input
        table.insert(content, "")
        table.insert(content, app.input:view())

        -- Add status
        local status_line = ""
        if app.is_processing then
            status_line = self.styles.system:render("Processing...")
        else
            if app.current_tool_use then
                status_line = self.styles.tool:render("Ctrl+T to execute tool | ")
                status_line = status_line ..
                self.styles.help:render("Enter to send | Ctrl+L to clear | Ctrl+D to toggle debug | Ctrl+C to quit")
            else
                status_line = self.styles.help:render(
                "Enter to send | Ctrl+L to clear | Ctrl+D to toggle debug | Ctrl+C to quit")
            end
        end
        table.insert(content, status_line)

        return self.styles.box
            :width(app.window.width - 2)
            :height(app.window.height - 2)
            :render(table.concat(content, "\n"))
    end

    -- Handle UI updates
    ui.update = function(self, app, msg)
        -- Update window size
        if msg.window_size then
            app.input:set_width(app.window.width - 8)
            app.debug_view:set_width(app.window.width - 8)
        end

        -- Update input
        local cmd = app.input:update(msg)
        if cmd then
            app:dispatch(cmd)
        end

        -- Update debug view
        local debug_cmd = app.debug_view:update(msg)
        if debug_cmd then
            app:dispatch(debug_cmd)
        end

        -- Handle keys
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.submit:matches(msg) and not app.is_processing then
                local user_input = app.input:value()
                if user_input ~= "" then
                    app.session:send_message(user_input)
                    app.input:set_value("")
                end
            elseif self.keys.clear:matches(msg) then
                app.session:clear_conversation()
                app:add_system_message("Conversation cleared")
            elseif self.keys.toggle_debug:matches(msg) then
                app.show_debug = not app.show_debug
                app:upstream("refresh")
            elseif self.keys.execute_tool:matches(msg) and not app.is_processing then
                -- Manual tool execution
                app.session:manual_execute_tool()
            end
        end

        return false
    end

    -- Add system message
    ui.add_system_message = function(self, app, message)
        table.insert(app.messages, {
            type = "system",
            content = message,
            timestamp = time.now()
        })
        app:upstream("refresh")
    end

    -- Add debug log
    ui.log_debug = function(self, app, message)
        if type(message) == "table" then
            message = json.encode(message)
        end

        local now = time.now()
        local entry = now:format("15:04:05") .. " " .. message

        table.insert(app.debug_logs, entry)

        -- Update debug viewport
        local debug_content = table.concat(app.debug_logs, "\n")
        app.debug_view:set_content(debug_content)
        app.debug_view:scroll_to_bottom()

        app:upstream("refresh")
    end

    return ui
end

return UI

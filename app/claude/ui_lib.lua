local json = require("json")
local time = require("time")

-- Enhanced UI Library for Claude TUI
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
        tool_block = btea.style()
            :foreground("#89DCEB")
            :background("#313244")
            :bold()
            :padding(0, 1),
        debug = btea.style()
            :foreground("#6C7086")
            :italic(),
        tab_active = btea.style()
            :padding(0, 2)
            :foreground("#CBA6F7")
            :background("#313244")
            :bold(),
        tab_inactive = btea.style()
            :padding(0, 2)
            :foreground("#6C7086")
            :background("#1E1E2E"),
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
        }),
        tab_next = btea.bind({
            keys = { "tab" },
            help = { key = "tab", desc = "next tab" }
        }),
        tab_prev = btea.bind({
            keys = { "shift+tab" },
            help = { key = "shift+tab", desc = "previous tab" }
        }),
        scroll_up = btea.bind({
            keys = { "ctrl+up", "ctrl+k" },
            help = { key = "^↑/^k", desc = "scroll up" }
        }),
        scroll_down = btea.bind({
            keys = { "ctrl+down", "ctrl+j" },
            help = { key = "^↓/^j", desc = "scroll down" }
        }),
        page_up = btea.bind({
            keys = { "pgup" },
            help = { key = "PgUp", desc = "page up" }
        }),
        page_down = btea.bind({
            keys = { "pgdn" },
            help = { key = "PgDn", desc = "page down" }
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

        -- Initialize message viewport for scrollable chat
        app.message_view = btea.viewport({
            width = app.window.width - 8,
            height = app.window.height - 14, -- Increased from 12 to 14 to add more space
            mouse_wheel_enabled = true,
            style = btea.style()
                :background("#1E1E2E")
        })

        -- Initialize viewport for debug logs (with full height)
        app.debug_view = btea.viewport({
            width = app.window.width - 8,
            height = app.window.height - 14, -- Same height as message view
            mouse_wheel_enabled = true
        })

        -- Initialize tab state
        app.active_tab = "chat" -- Options: "chat", "debug", "info"

        -- Focus the input immediately
        app:dispatch(app.input:focus())

        return app
    end

    -- Format tool calls as styled blocks (using Lip Gloss styling features)
    ui.format_tool_call = function(self, tool_name)
        -- Create a stylish tool indicator with custom styling
        local custom_border = {
            left = "┃",
            right = ""
        }

        -- Create a specialized tool style with custom border and colors
        local tool_style = btea.style()
            :foreground("#89DCEB")
            :background("#313244")
            :bold()
            :padding(0, 1)
            :custom_border(custom_border)

        return tool_style:render(" " .. tool_name .. " ")
    end

    -- Render tab bar
    ui.render_tabs = function(self, app)
        local tabs = {
            { id = "chat", name = "Chat" },
            { id = "debug", name = "Debug" },
            { id = "info", name = "Info" }
        }

        local rendered_tabs = {}
        for _, tab in ipairs(tabs) do
            local style = tab.id == app.active_tab
                       and self.styles.tab_active
                       or self.styles.tab_inactive
            table.insert(rendered_tabs, style:render(tab.name))
        end

        return table.concat(rendered_tabs, " ")
    end

    -- Update the message viewport with formatted messages
    ui.update_message_view = function(self, app)
        local content = {}

        for i, msg in ipairs(app.messages) do
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
            elseif msg.type == "error" then
                prefix = "Error: "
            end

            local line
            if msg.type == "tool" then
                -- Format tool calls as special blocks (only show tool name) - inline with timestamp
                local tool_name = msg.tool_name or "Tool"
                line = styled_time .. " " .. self:format_tool_call(tool_name)
            else
                -- Format other messages normally
                line = styled_time .. " " .. style:render(prefix .. msg.content)
            end

            table.insert(content, line)
        end

        -- Update the viewport with the formatted content - use single newline instead of double
        app.message_view:set_content(table.concat(content, "\n"))

        -- Auto-scroll to bottom if we were already at the bottom
        if app.message_view:at_bottom() then
            app.message_view:scroll_to_bottom()
        end
    end

    -- Main render function
    ui.render = function(self, app)
        local content_width = app.window.width - 6

        -- Prepare header with tabs
        local tabs = self:render_tabs(app)
        local header = self.styles.header:render("Claude Terminal Interface") .. "\n" .. tabs
        local header_divider = string.rep("═", content_width)

        -- Prepare content based on active tab
        local main_content
        if app.active_tab == "chat" then
            -- Ensure messages are up to date in the viewport
            self:update_message_view(app)
            main_content = app.message_view:view()
        elseif app.active_tab == "debug" then
            main_content = app.debug_view:view()
        elseif app.active_tab == "info" then
            -- Info tab content
            local info_lines = {
                "Claude Version: " .. app.client.MODEL,
                "API Base URL: " .. app.client.API_URL,
                "API Version: " .. app.client.API_VERSION,
                "Max Tokens: " .. app.client.MAX_TOKENS,
                "",
                "Available Tools:",
            }

            -- Add tool information
            for _, tool in ipairs(app.agent_handler.tools) do
                table.insert(info_lines, "- " .. tool.name .. ": " .. tool.description)
            end

            main_content = table.concat(info_lines, "\n")
        end

        -- Prepare footer with input and help - add extra padding line for clear separation
        local input_line = app.input:view()

        -- Add status
        local status_line = ""
        if app.is_processing then
            status_line = self.styles.system:render("Processing...")
        else
            if app.current_tool_use then
                status_line = self.styles.tool:render("Ctrl+T to execute tool | ")
            end

            local help_text = {}
            table.insert(help_text, "Enter to send")
            table.insert(help_text, "Ctrl+L to clear")
            table.insert(help_text, "Tab to switch tabs")
            table.insert(help_text, "PgUp/PgDn to scroll")
            table.insert(help_text, "Ctrl+D to toggle debug")
            table.insert(help_text, "Ctrl+C to quit")

            status_line = status_line .. self.styles.help:render(table.concat(help_text, " | "))
        end

        -- Combine all parts with explicit padding line before input
        local all_content = {
            header,
            self.styles.timestamp:render(header_divider),
            "",
            main_content,
            "",  -- First padding line
            "",  -- Second padding line for better separation
            input_line,
            status_line
        }

        return self.styles.box
            :width(app.window.width - 2)
            :height(app.window.height - 2)
            :render(table.concat(all_content, "\n"))
    end

    -- Handle UI updates
    ui.update = function(self, app, msg)
        -- Update window size
        if msg.window_size then
            local viewport_height = app.window.height - 14 -- Increased from 12 to 14
            app.input:set_width(app.window.width - 8)
            app.message_view:set_width(app.window.width - 8)
            app.message_view:set_height(viewport_height)
            app.debug_view:set_width(app.window.width - 8)
            app.debug_view:set_height(viewport_height)
        end

        -- Update input
        local cmd = app.input:update(msg)
        if cmd then
            app:dispatch(cmd)
        end

        -- Update viewports
        local message_cmd = app.message_view:update(msg)
        if message_cmd then
            app:dispatch(message_cmd)
        end

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
            elseif self.keys.tab_next:matches(msg) then
                -- Cycle to next tab
                if app.active_tab == "chat" then
                    app.active_tab = "debug"
                elseif app.active_tab == "debug" then
                    app.active_tab = "info"
                else
                    app.active_tab = "chat"
                end
                app:upstream("refresh")
            elseif self.keys.tab_prev:matches(msg) then
                -- Cycle to previous tab
                if app.active_tab == "chat" then
                    app.active_tab = "info"
                elseif app.active_tab == "debug" then
                    app.active_tab = "chat"
                else
                    app.active_tab = "debug"
                end
                app:upstream("refresh")
            elseif self.keys.scroll_up:matches(msg) then
                -- Scroll up in the active viewport
                if app.active_tab == "chat" then
                    app:dispatch(app.message_view:line_up(1))
                elseif app.active_tab == "debug" then
                    app:dispatch(app.debug_view:line_up(1))
                end
            elseif self.keys.scroll_down:matches(msg) then
                -- Scroll down in the active viewport
                if app.active_tab == "chat" then
                    app:dispatch(app.message_view:line_down(1))
                elseif app.active_tab == "debug" then
                    app:dispatch(app.debug_view:line_down(1))
                end
            elseif self.keys.page_up:matches(msg) then
                -- Page up in the active viewport
                if app.active_tab == "chat" then
                    app:dispatch(app.message_view:page_up())
                elseif app.active_tab == "debug" then
                    app:dispatch(app.debug_view:page_up())
                end
            elseif self.keys.page_down:matches(msg) then
                -- Page down in the active viewport
                if app.active_tab == "chat" then
                    app:dispatch(app.message_view:page_down())
                elseif app.active_tab == "debug" then
                    app:dispatch(app.debug_view:page_down())
                end
            end
        end

        -- Handle mouse wheel for scrolling
        if type(msg) == "table" and msg.type == "update" and msg.mouse then
            if msg.mouse.button == "wheel_up" then
                if app.active_tab == "chat" then
                    app:dispatch(app.message_view:line_up(3))
                elseif app.active_tab == "debug" then
                    app:dispatch(app.debug_view:line_up(3))
                end
            elseif msg.mouse.button == "wheel_down" then
                if app.active_tab == "chat" then
                    app:dispatch(app.message_view:line_down(3))
                elseif app.active_tab == "debug" then
                    app:dispatch(app.debug_view:line_down(3))
                end
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
        self:update_message_view(app)
        app:upstream("refresh")
    end

    -- Add tool message
    ui.add_tool_message = function(self, app, message, tool_name)
        table.insert(app.messages, {
            type = "tool",
            content = message,
            tool_name = tool_name or "unknown tool",
            timestamp = time.now()
        })

        -- Log the actual result in debug
        self:log_debug(app, "Tool result: " .. (message and message:sub(1, 100) or "nil") ..
            (message and message:len() > 100 and "..." or ""))

        -- Update and scroll
        self:update_message_view(app)
        app.message_view:scroll_to_bottom()

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
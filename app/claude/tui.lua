local bapp = require("bapp")
local time = require("time")
local json = require("json")
local funcs = require("funcs")
local claude_client = require("claude_client")
local agent_lib = require("agent_lib")

-- Claude TUI Application
function App()
    -- Create app with proper init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }
    local app = bapp.new(init_commands)

    -- Initialize state
    app.messages = {}
    app.update_channel = channel.new()
    app.tools_channel = channel.new()
    app.session_id = nil
    app.is_processing = false

    -- Create shared inbox to prevent multiple subscriptions
    app.shared_inbox = process.inbox()

    -- Track whether we're already listening for updates
    app.listening_for_updates = false
    app.listening_for_tools = false

    -- Create Claude client
    app.claude = claude_client.Client.new()

    -- Create agent with tools
    app.agent = agent_lib.create_interactive("tool_use")

    -- Initialize text input with proper width
    app.input = btea.text_input({
        prompt = "> ",
        placeholder = "Ask Claude something...",
        width = app.window.width - 8
    })

    -- Focus the input immediately
    app:dispatch(app.input:focus())

    -- Define key bindings
    app.keys = {
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
        })
    }

    -- Define styles
    app.styles = {
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
        tool = btea.style()
            :foreground("#89DCEB"),
        timestamp = btea.style()
            :foreground("#6C7086"),
        error = btea.style()
            :foreground("#F38BA8")
            :italic(),
        help = btea.style()
            :foreground("#6C7086")
            :italic()
    }

    -- Add system message helper
    function app:add_system_message(message)
        table.insert(app.messages, {
            type = "system",
            content = message,
            timestamp = time.now()
        })
        app:upstream("refresh")
    end

    -- Create a session with the Claude service
    function app:create_session()
        -- No need to create a new inbox here, use the shared one
        local inbox = app.shared_inbox

        -- Send create session request
        local ok = process.send("{Antares@system:heap|claude:llm.service|0x00001}", "create_session", {
            from = process.pid(),
            reply_to = process.pid(),
            config = {
                system = app.agent:get_system_prompt(),
                tools = app.agent:get_tools()
            }
        })

        if not ok then
            app:add_system_message("Failed to contact Claude service")
            return
        end

        -- Wait for response with timeout
        local timeout = time.after("5s")
        local result = channel.select({
            inbox:case_receive(),
            timeout:case_receive()
        })

        if result.channel == timeout then
            app:add_system_message("Session creation timeout")
            return
        end

        if result.value.topic == "session_created" then
            app.session_id = result.value.payload.session_id
            app:add_system_message("Connected to Claude (Session ID: " .. app.session_id .. ")")
        else
            app:add_system_message("Failed to create session: " .. json.encode(result.value))
        end
    end

    -- Send message to Claude
    function app:send_to_claude(message)
        if not app.session_id then
            app:add_system_message("No active session, creating one...")
            app:create_session()

            if not app.session_id then
                app:add_system_message("Failed to create session")
                return
            end
        end

        -- Use the shared inbox - no need to create a new one
        local inbox = app.shared_inbox

        -- Send message to Claude service
        local ok = process.send("{Antares@system:heap|claude:llm.service|0x00001}", "send_message", {
            from = process.pid(),
            reply_to = process.pid(),
            session_id = app.session_id,
            content = { { type = "text", text = message } },
            stream = true
        })

        if not ok then
            app:add_system_message("Failed to send message to Claude service")
            return
        end

        -- Process streaming responses - Only start the listener if not already listening
        if not app.listening_for_updates then
            app.listening_for_updates = true

            coroutine.spawn(function()
                while true do
                    local result = channel.select({
                        inbox:case_receive()
                    })

                    if not result.ok then
                        break
                    end

                    local msg = result.value
                    if msg.topic == "update" then
                        app.update_channel:send({
                            type = "update",
                            text = msg.payload.text
                        })
                    elseif msg.topic == "tool_use" then
                        app.update_channel:send({
                            type = "tool_use",
                            tool_use_id = msg.payload.tool_use_id,
                            name = msg.payload.name,
                            input = msg.payload.input
                        })
                    elseif msg.topic == "done" then
                        app.update_channel:send({
                            type = "done"
                        })
                    elseif msg.topic == "error" then
                        app.update_channel:send({
                            type = "error",
                            error = msg.payload.message
                        })
                    end
                end
            end)
        end
    end

    -- Submit tool result to Claude
    function app:submit_tool_result(tool_use_id, result, error_message)
        if not app.session_id then
            app:add_system_message("No active session")
            return
        end

        -- Use the shared inbox - no need to create a new one
        local inbox = app.shared_inbox

        -- Create message object
        local msg = {
            from = process.pid(),
            reply_to = process.pid(),
            session_id = app.session_id,
            tool_use_id = tool_use_id,
            stream = true
        }

        if error_message then
            msg.error = error_message
        else
            msg.result = result
        end

        -- Send tool result to Claude service
        local ok = process.send("{Antares@system:heap|claude:llm.service|0x00001}", "submit_tool_result", msg)

        if not ok then
            app:add_system_message("Failed to submit tool result to Claude service")
            return
        end
    end

    -- Execute tool function
    function app:execute_tool(tool_request)
        local executor = agent_lib.tools_executors[tool_request.name]
        if not executor then
            -- Try to find a function with this name
            local funcs_executor = funcs.new()

            -- Call the function
            local result, err = funcs_executor:call(tool_request.name, tool_request.args)
            if err then
                return nil, "Tool execution failed: " .. err
            end

            if type(result) == "table" then
                return json.encode(result)
            else
                return tostring(result)
            end
        else
            -- Use the defined executor
            return executor(tool_request.args)
        end
    end

    -- Now that all methods are defined, create a session
    app:create_session()

    -- Start tool execution background worker - Only start if not already running
    if not app.listening_for_tools then
        app.listening_for_tools = true

        coroutine.spawn(function()
            while true do
                local tool_request, ok = app.tools_channel:receive()
                if not ok then break end

                -- Add system message about tool execution
                local system_msg = {
                    type = "system",
                    content = "Executing tool: " .. tool_request.name,
                    timestamp = time.now()
                }
                table.insert(app.messages, system_msg)
                app:upstream("refresh")

                -- Execute the tool
                local result, err = app:execute_tool(tool_request)

                -- Add tool result message
                local result_msg = {
                    type = "tool",
                    content = err and ("Error: " .. err) or result,
                    tool_name = tool_request.name,
                    timestamp = time.now()
                }
                table.insert(app.messages, result_msg)

                -- Submit tool result back to Claude
                app:submit_tool_result(tool_request.id, result, err)

                app:upstream("refresh")
            end
        end)
    end

    -- Start LLM update listener
    coroutine.spawn(function()
        while true do
            local msg, ok = app.update_channel:receive()
            if not ok then break end

            if msg.type == "update" then
                -- Update the last assistant message
                local last_index = #app.messages
                if last_index > 0 and app.messages[last_index].type == "assistant" then
                    app.messages[last_index].content =
                        app.messages[last_index].content .. msg.text
                else
                    -- Add new assistant message
                    table.insert(app.messages, {
                        type = "assistant",
                        content = msg.text,
                        timestamp = time.now()
                    })
                end

                app:upstream("refresh")
            elseif msg.type == "tool_use" then
                -- Handle tool use request
                table.insert(app.messages, {
                    type = "system",
                    content = "Claude wants to use tool: " .. msg.name,
                    timestamp = time.now()
                })

                -- Send to tools channel
                app.tools_channel:send({
                    id = msg.tool_use_id,
                    name = msg.name,
                    args = msg.input
                })

                app:upstream("refresh")
            elseif msg.type == "done" then
                -- Reset processing state
                app.is_processing = false
                app:upstream("refresh")
            elseif msg.type == "error" then
                table.insert(app.messages, {
                    type = "error",
                    content = tostring(msg.error),
                    timestamp = time.now()
                })
                app.is_processing = false
                app:upstream("refresh")
            end
        end
    end)

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            self.input:set_width(self.window.width - 8)
        end

        -- Update text input
        local cmd = self.input:update(msg)
        if cmd then
            self:dispatch(cmd)
        end

        -- Handle key events
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.submit:matches(msg) and not self.is_processing then
                local user_input = self.input:value()
                if user_input ~= "" then
                    -- Add user message
                    table.insert(self.messages, {
                        type = "user",
                        content = user_input,
                        timestamp = time.now()
                    })

                    -- Send to Claude
                    self:send_to_claude(user_input)

                    -- Clear input field
                    self.input:set_value("")

                    -- Set processing flag
                    self.is_processing = true

                    -- Add assistant message with ellipsis to show it's thinking
                    table.insert(self.messages, {
                        type = "assistant",
                        content = "...",
                        timestamp = time.now()
                    })
                end
            elseif self.keys.clear:matches(msg) then
                self.messages = {}

                -- Reset session
                if self.session_id then
                    -- No need to create a new inbox here, use the shared one
                    local inbox = self.shared_inbox

                    -- Send clear session request
                    process.send("{Antares@system:heap|claude:llm.service|0x00001}", "clear_session", {
                        from = process.pid(),
                        reply_to = process.pid(),
                        session_id = self.session_id
                    })

                    -- Wait for response with timeout
                    local timeout = time.after("5s")
                    channel.select({
                        inbox:case_receive(),
                        timeout:case_receive()
                    })

                    self:add_system_message("Session cleared")
                end
            end
        end

        return false
    end

    -- View rendering
    local function view(self)
        local content_width = self.window.width - 6
        local header_divider = string.rep("═", content_width)
        local content = {
            self.styles.header:render("Claude Terminal Interface"),
            self.styles.timestamp:render(header_divider)
        }

        -- Calculate visible messages area
        local max_visible = self.window.height - 8
        local start_idx = math.max(1, #self.messages - max_visible)

        -- Add messages with timestamps
        for i = start_idx, #self.messages do
            local msg = self.messages[i]
            local timestamp = msg.timestamp:format("15:04:05")
            local styled_time = self.styles.timestamp:render(timestamp)
            local style = self.styles[msg.type]

            -- Format based on message type
            local prefix = ""
            if msg.type == "user" then
                prefix = "You: "
            elseif msg.type == "assistant" then
                prefix = "Claude: "
            elseif msg.type == "system" then
                prefix = "System: "
            elseif msg.type == "tool" then
                prefix = "Tool [" .. (msg.tool_name or "unknown") .. "]: "
            end

            local styled_text = style:render(prefix .. msg.content)
            table.insert(content, styled_time .. " " .. styled_text)
        end

        -- Add input field
        table.insert(content, "")
        table.insert(content, self.input:view())

        -- Add status line
        local status_line = ""
        if self.is_processing then
            status_line = self.styles.system:render("Processing...")
        else
            status_line = self.styles.help:render("Enter to send | Ctrl+L to clear | Ctrl+C to quit")
        end
        table.insert(content, status_line)

        return self.styles.box
            :width(self.window.width - 2)
            :height(self.window.height - 2)
            :render(table.concat(content, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App
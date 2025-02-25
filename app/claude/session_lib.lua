local json = require("json")
local time = require("time")

-- Session Manager for Claude TUI
local Session = {}

function Session.new(app, client, agent_handler)
    local session = {}

    -- Initialize session state
    session.messages = app.messages
    session.conversation_history = {}
    session.debug_logs = app.debug_logs
    session.is_processing = false
    session.current_tool_use = nil

    -- Store references to other components
    session.app = app
    session.client = client
    session.agent_handler = agent_handler

    -- Add a user message
    session.add_user_message = function(self, message)
        table.insert(self.messages, {
            type = "user",
            content = message,
            timestamp = time.now()
        })

        -- Add message to history
        table.insert(self.conversation_history, {
            role = "user",
            content = {
                { type = "text", text = message }
            }
        })

        self.app:upstream("refresh")
        return self
    end

    -- Add an assistant message
    session.add_assistant_message = function(self, message)
        table.insert(self.messages, {
            type = "assistant",
            content = message or "Thinking...",
            timestamp = time.now()
        })

        self.app:upstream("refresh")
        return self
    end

    -- Add a tool message - now only shows that a tool was executed without the full content
    session.add_tool_message = function(self, message, tool_name)
        -- Instead of showing full content, just show a notification that tool was executed
        local tool_notification = "Tool executed successfully: " .. (tool_name or "unknown tool")

        table.insert(self.messages, {
            type = "tool",
            content = tool_notification,
            timestamp = time.now()
        })

        -- Log the actual result in debug but don't show it in the UI
        if message then
            self.app.ui:log_debug(self.app, "Tool result (hidden from UI): " .. message:sub(1, 100) ..
                (message:len() > 100 and "..." or ""))
        end

        self.app:upstream("refresh")
        return self
    end

    -- Send a message to Claude
    session.send_message = function(self, message)
        self.is_processing = true

        -- Add user message to UI
        self:add_user_message(message)

        -- Add thinking message
        self:add_assistant_message("Thinking...")

        -- Send to Claude with history
        self:send_to_claude_with_history()

        return self
    end

    -- Submit tool result
    session.submit_tool_result = function(self, tool_use_id, result)
        self.app.ui:log_debug(self.app, "Submitting tool result for ID: " .. tool_use_id)

        -- Create message with correct structure
        local tool_result_message = {
            role = "user",
            content = {
                {
                    type = "tool_result",
                    tool_use_id = tool_use_id,
                    content = result
                }
            }
        }

        self.app.ui:log_debug(self.app, "Tool result message: " .. json.encode(tool_result_message))

        -- Add to history with the same structure
        table.insert(self.conversation_history, tool_result_message)

        -- Reset current tool use
        self.current_tool_use = nil

        -- Send to Claude
        self:send_to_claude_with_history()
    end

    -- Execute a tool
    session.execute_tool = function(self, tool_name, tool_input, tool_use_id)
        self.app.ui:log_debug(self.app, "Executing tool: " .. tool_name .. " with input: " .. json.encode(tool_input))

        -- Check if we have a valid tool_use_id
        if not tool_use_id or tool_use_id == "" then
            self.app.ui:add_system_message(self.app, "Cannot execute tool: missing tool ID")
            return
        end

        -- Add system message
        self.app.ui:add_system_message(self.app, "Executing tool: " .. tool_name)

        -- Execute the tool using agent handler
        local result, err = self.agent_handler:execute_tool(tool_name, tool_input)

        if err then
            self.app.ui:add_system_message(self.app, "Tool execution failed: " .. err)
            return
        end

        -- Add tool result message (now just showing that tool executed)
        self:add_tool_message(result, tool_name)

        -- Submit tool result
        self:submit_tool_result(tool_use_id, result)

        return result
    end

    -- Handle manual tool execution when Claude fails
    session.manual_execute_tool = function(self)
        if not self.current_tool_use then
            self.app.ui:add_system_message(self.app, "No tool to execute")
            return
        end

        local tool = self.current_tool_use
        self:execute_tool(tool.name, tool.input, tool.id)
    end

    -- Send message to Claude with full conversation history
    session.send_to_claude_with_history = function(self)
        -- Make a clean copy of conversation history
        local sanitized_messages = {}
        for _, msg in ipairs(self.conversation_history) do
            -- Simple deep copy to avoid reference issues
            local sanitized = {
                role = msg.role,
                content = {}
            }

            -- Copy content exactly as is
            for i, block in ipairs(msg.content) do
                table.insert(sanitized.content, block)
            end

            table.insert(sanitized_messages, sanitized)
        end

        -- Send to client
        self.client:send_request(self.app.agent_handler:get_tools(), sanitized_messages, function(stream)
            self:process_stream(stream)
        end)
    end

    -- Process streaming response
    session.process_stream = function(self, stream)
        local current_response = {
            role = "assistant",
            content = {}
        }
        local is_done = false
        local accumulated_json = ""

        -- Find last assistant message
        local last_message_index = #self.messages
        if last_message_index > 0 and self.messages[last_message_index].type == "assistant" then
            -- Will update this message
        else
            -- Add new message
            table.insert(self.messages, {
                type = "assistant",
                content = "",
                timestamp = time.now()
            })
            last_message_index = #self.messages
        end

        -- Process stream
        while not is_done do
            local chunk = stream:read()
            if not chunk then break end

            for line in chunk:gmatch("[^\r\n]+") do
                if line:sub(1, 6) == "data: " then
                    local data = line:sub(7)

                    -- Check for end
                    if data == "[DONE]" then
                        is_done = true
                        break
                    end

                    -- Parse JSON
                    local success, event = pcall(json.decode, data)
                    if success and event then
                        -- Log full event
                        self.app.ui:log_debug(self.app, "Event: " .. json.encode(event))

                        if event.type == "message_start" then
                            current_response.role = event.message.role
                        elseif event.type == "content_block_start" then
                            if event.content_block.type == "tool_use" then
                                -- Directly capture tool_use block with ID
                                self.app.ui:log_debug(self.app, "Tool use block started: " .. json.encode(event.content_block))

                                -- Store both in current response and for tool execution
                                table.insert(current_response.content, {
                                    type = "tool_use",
                                    id = event.content_block.id,
                                    name = event.content_block.name,
                                    input = event.content_block.input or {}
                                })

                                -- Save for potential execution
                                self.current_tool_use = {
                                    id = event.content_block.id,
                                    name = event.content_block.name,
                                    input = event.content_block.input or {}
                                }
                            else
                                table.insert(current_response.content, {
                                    type = event.content_block.type,
                                    text = ""
                                })
                            end
                        elseif event.type == "content_block_delta" then
                            if event.delta.type == "text_delta" then
                                -- Handle text updates
                                local last_block = current_response.content[#current_response.content]
                                if last_block and last_block.type == "text" then
                                    last_block.text = last_block.text .. (event.delta.text or "")

                                    if self.messages[last_message_index].content == "Thinking..." then
                                        self.messages[last_message_index].content = event.delta.text
                                    else
                                        self.messages[last_message_index].content = self.messages[last_message_index].content .. event.delta.text
                                    end

                                    self.app:upstream("refresh")
                                end
                            elseif event.delta.type == "input_json_delta" then
                                -- Handle JSON delta for tool_use input
                                self.app.ui:log_debug(self.app, "JSON delta: " .. json.encode(event.delta))
                                local partial_json = event.delta.partial_json or ""
                                accumulated_json = accumulated_json .. partial_json

                                -- Try to parse the JSON if it looks complete
                                if accumulated_json:match("^%s*{.*}%s*$") then
                                    local success, parsed = pcall(json.decode, accumulated_json)
                                    if success and parsed then
                                        -- Update tool input
                                        for i, block in ipairs(current_response.content) do
                                            if block.type == "tool_use" then
                                                -- Update with parsed JSON
                                                block.input = parsed

                                                -- Also update current tool use
                                                if self.current_tool_use and self.current_tool_use.id == block.id then
                                                    self.current_tool_use.input = parsed
                                                end

                                                break
                                            end
                                        end

                                        -- Reset accumulated JSON
                                        accumulated_json = ""
                                    end
                                end
                            end
                        elseif event.type == "content_block_stop" or event.type == "message_stop" then
                            -- If we have a complete tool use request, add it to history before executing
                            if event.type == "message_stop" and self.current_tool_use then
                                -- Add assistant message to history BEFORE executing tool
                                table.insert(self.conversation_history, current_response)

                                -- Now execute the tool with all necessary info
                                if self.current_tool_use.id and self.current_tool_use.name and self.current_tool_use.input then
                                    coroutine.spawn(function()
                                        self:execute_tool(self.current_tool_use.name, self.current_tool_use.input, self.current_tool_use.id)
                                    end)
                                end
                            end
                        end
                    end
                end
            end
        end

        -- Add to conversation history if not already added
        if #current_response.content > 0 and not self.current_tool_use then
            table.insert(self.conversation_history, current_response)
        end

        -- Clean up
        stream:close()
        self.is_processing = false
        self.app:upstream("refresh")
    end

    -- Clear conversation
    session.clear_conversation = function(self)
        self.messages = {}
        self.conversation_history = {}
        self.debug_logs = {}
        self.app.debug_view:set_content("")
        self.current_tool_use = nil
        return self
    end

    return session
end

return Session
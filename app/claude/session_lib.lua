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
    session.waiting_for_tool_result = false  -- Flag to prevent concurrent processing
    session.processed_message_ids = {}  -- Track which messages have been added to history

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

        -- Update UI and scroll to bottom
        self.app.ui:update_message_view(self.app)
        self.app.message_view:scroll_to_bottom()

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

        -- Update viewport and scroll to bottom
        self.app.ui:update_message_view(self.app)
        self.app.message_view:scroll_to_bottom()

        self.app:upstream("refresh")
        return self
    end

    -- Add a tool message - now only shows that a tool was executed without the full content
    session.add_tool_message = function(self, message, tool_name)
        -- Create a simple tool notification, we'll only show the tool name in the UI
        local tool_notification = message or "Tool executed"

        table.insert(self.messages, {
            type = "tool",
            content = tool_notification, -- Content won't be displayed in UI
            tool_name = tool_name or "unknown tool",
            timestamp = time.now()
        })

        -- Log the actual result in debug but don't show it in the UI
        if message then
            self.app.ui:log_debug(self.app, "Tool result (hidden from UI): " .. message:sub(1, 100) ..
                (message:len() > 100 and "..." or ""))
        end

        -- Update UI and scroll to bottom
        self.app.ui:update_message_view(self.app)
        self.app.message_view:scroll_to_bottom()

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

        -- Ensure result is properly formatted for Claude
        local content_value

        -- If it's already a string, use it directly
        if type(result) == "string" then
            -- Check if it looks like JSON
            if result:sub(1, 1) == "{" and result:sub(-1) == "}" then
                -- It's already JSON-formatted, use as is
                content_value = result
            else
                -- Plain text, so wrap it
                content_value = result
            end
        else
            -- It's a table, encode it to JSON
            local success, encoded = pcall(json.encode, result)
            if success then
                content_value = encoded
            else
                -- Fallback if encoding fails
                content_value = "Error: Failed to encode result"
            end
        end

        -- Create message with correct structure
        local tool_result_message = {
            role = "user",
            content = {
                {
                    type = "tool_result",
                    tool_use_id = tool_use_id,
                    content = content_value
                }
            }
        }

        -- Log a short preview of the message
        self.app.ui:log_debug(self.app, "Tool result preview: " .. tostring(content_value):sub(1, 50) ..
                             (tostring(content_value):len() > 50 and "..." or ""))

        -- Add to history
        table.insert(self.conversation_history, tool_result_message)

        -- Reset current tool use and flags
        self.current_tool_use = nil
        self.waiting_for_tool_result = false  -- Reset flag so processing can continue

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

            -- Create a proper error result
            result = {
                error = err,
                status = "error",
                message = "The tool execution failed: " .. err
            }
        end

        -- Add tool result message with the tool name, but don't display the full content in the UI
        self:add_tool_message("Tool executed successfully", tool_name)

        -- Always submit result to Claude
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
        -- Debug the history first
        self.app.ui:log_debug(self.app, "Sending history with " .. #self.conversation_history .. " messages")

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
            -- if string report to debug as error
            if type(stream) == "string" then
                self.app.ui:log_debug(self.app, "Error: " .. stream)
                return
            end

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

        -- Make sure we scroll to the bottom when receiving new content
        self.app.message_view:scroll_to_bottom()

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
                            current_response.id = event.message.id  -- Store message ID
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

                                        -- Update UI and auto-scroll
                                        self.app.ui:update_message_view(self.app)
                                        self.app.message_view:scroll_to_bottom()

                                    else
                                        self.messages[last_message_index].content = self.messages[last_message_index].content .. event.delta.text

                                        -- Update UI and auto-scroll
                                        self.app.ui:update_message_view(self.app)
                                        self.app.message_view:scroll_to_bottom()
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
                            if event.type == "message_stop" and self.current_tool_use and not self.waiting_for_tool_result then
                                -- Set flag to prevent concurrent processing
                                self.waiting_for_tool_result = true

                                -- Only add message to history if we haven't processed it before
                                if current_response.id and not self.processed_message_ids[current_response.id] then
                                    self.processed_message_ids[current_response.id] = true
                                    self.app.ui:log_debug(self.app, "Adding message ID to history: " .. current_response.id)
                                    table.insert(self.conversation_history, current_response)
                                else
                                    self.app.ui:log_debug(self.app, "Skipping duplicate message")
                                end

                                -- Now execute the tool SYNCHRONOUSLY - no coroutine.spawn
                                if self.current_tool_use.id and self.current_tool_use.name and self.current_tool_use.input then
                                    self:execute_tool(self.current_tool_use.name, self.current_tool_use.input, self.current_tool_use.id)
                                end
                            end
                        end
                    end
                end
            end
        end

        -- Add to conversation history if not already added and not a tool_use
        if #current_response.content > 0 and not self.waiting_for_tool_result then
            -- Only add if we haven't processed this message before
            if current_response.id and not self.processed_message_ids[current_response.id] then
                self.processed_message_ids[current_response.id] = true
                self.app.ui:log_debug(self.app, "Adding message ID to history at end: " .. current_response.id)
                table.insert(self.conversation_history, current_response)
            end
        end

        -- Clean up
        stream:close()
        self.is_processing = false

        -- Final update and auto-scroll
        self.app.ui:update_message_view(self.app)
        self.app.message_view:scroll_to_bottom()

        self.app:upstream("refresh")
    end

    -- Clear conversation
    session.clear_conversation = function(self)
        self.messages = {}
        self.conversation_history = {}
        self.debug_logs = {}
        self.app.debug_view:set_content("")
        self.current_tool_use = nil
        self.waiting_for_tool_result = false
        self.processed_message_ids = {}  -- Reset processed message IDs

        -- Update UI and scroll
        self.app.ui:update_message_view(self.app)

        return self
    end

    return session
end

return Session
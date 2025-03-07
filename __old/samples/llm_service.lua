local actor = require("actor")
local time = require("time")
local json = require("json")
local http_client = require("http_client")
local env = require("env")

local function extract_error_details(response)
    if not response then
        return "No response received"
    end

    -- If we have a response body, try to parse it as JSON first
    if response.body and #response.body > 0 then
        local success, decoded = pcall(json.decode, response.body)
        if success and decoded and decoded.error then
            -- Return the detailed error message from the API
            return "API error: " .. response.status_code .. " - " ..
                (decoded.error.message or decoded.error.type or json.encode(decoded.error))
        end

        -- Otherwise just include the raw body
        return "API error: " .. response.status_code .. " - " .. response.body
    end

    -- If we have a stream but no body, try to read the first chunk
    if response.stream then
        local error_body = ""
        local chunk = response.stream:read()
        if chunk then
            error_body = chunk

            -- Try to parse as JSON
            local success, decoded = pcall(json.decode, error_body)
            if success and decoded and decoded.error then
                return "API error: " .. response.status_code .. " - " ..
                    (decoded.error.message or decoded.error.type or json.encode(decoded.error))
            end

            -- Otherwise use the raw chunk
            if #error_body > 0 then
                return "API error: " .. response.status_code .. " - " .. error_body
            end
        end
    end

    -- Fallback to basic error message
    return "API error: " .. response.status_code
end

-- Claude LLM Service implementation
local function run()
    -- Initialize state
    local state = {
        pid = process.pid(),
        active_sessions = {},
        next_session_id = 1
    }

    print("Claude LLM service started with PID:", state.pid)

    -- Create the actor with message handlers
    local llm_service = actor.new(state, {
        -- Create a new session
        create_session = function(state, msg)
            local session_id = state.next_session_id
            state.next_session_id = state.next_session_id + 1

            -- Create session with provided config or defaults
            local config = msg.config or {}
            local api_key = config.api_key or env.get("ANTHROPIC_API_KEY")

            if not api_key then
                if msg.reply_to then
                    process.send(msg.reply_to, "error", {
                        message = "No API key provided and ANTHROPIC_API_KEY environment variable not set"
                    })
                end
                return
            end

            -- Create session
            state.active_sessions[session_id] = {
                id = session_id,
                created_at = time.now(),
                last_active = time.now(),
                config = config,
                api_key = api_key,
                messages = {},
                tools = config.tools or {}
            }

            -- Return session ID to caller
            if msg.reply_to then
                process.send(msg.reply_to, "session_created", {
                    session_id = session_id
                })
            end

            print("Created session:", session_id)
        end,

        -- Send a message to Claude
        send_message = function(state, msg)
            local session_id = msg.session_id
            local session = state.active_sessions[session_id]

            if not session then
                if msg.reply_to then
                    process.send(msg.reply_to, "error", {
                        message = "Session not found: " .. tostring(session_id)
                    })
                end
                return
            end

            -- Update last active timestamp
            session.last_active = time.now()

            -- Add the user message to session history
            table.insert(session.messages, {
                role = "user",
                content = msg.content
            })

            -- Make API call to Claude
            local claude_request = {
                model = session.config.model or "claude-3-7-sonnet-20250219",
                max_tokens = session.config.max_tokens or 4096,
                temperature = session.config.temperature or 0.7,
                messages = session.messages
            }

            -- Add system prompt if configured
            if session.config.system then
                claude_request.system = session.config.system
            end

            -- Add tools if available
            if #session.tools > 0 then
                claude_request.tools = session.tools
            end

            -- Add tool_choice if specified
            if session.config.tool_choice then
                claude_request.tool_choice = session.config.tool_choice
            end

            -- Spawn coroutine to make API call
            coroutine.spawn(function()
                local headers = {
                    ["Content-Type"] = "application/json",
                    ["x-api-key"] = session.api_key,
                    ["anthropic-version"] = "2023-06-01"
                }

                local api_url = "https://api.anthropic.com/v1/messages"

                -- Check if streaming is enabled
                if msg.stream and msg.reply_to then
                    -- Add streaming parameter
                    claude_request.stream = true

                    local response, err = http_client.post(api_url, {
                        headers = headers,
                        body = json.encode(claude_request),
                        stream = { buffer_size = 4096 }
                    })

                    if err then
                        process.send(msg.reply_to, "error", {
                            message = "API request failed: " .. err
                        })
                        return
                    end

                    if response.status_code < 200 or response.status_code >= 300 then
                        -- Use improved error extraction
                        local error_message = extract_error_details(response)
                        process.send(msg.reply_to, "error", {
                            message = error_message
                        })
                        return
                    end

                    -- Process streaming response
                    process_streaming_response(msg.reply_to, response.stream, session)
                else
                    -- Non-streaming request
                    local response, err = http_client.post(api_url, {
                        headers = headers,
                        body = json.encode(claude_request),
                        timeout = 120
                    })

                    if err then
                        if msg.reply_to then
                            process.send(msg.reply_to, "error", {
                                message = "API request failed: " .. err
                            })
                        end
                        return
                    end

                    if response.status_code < 200 or response.status_code >= 300 then
                        -- Guard against nil response.body with empty string fallback
                        local body_text = response.body or ""
                        if msg.reply_to then
                            process.send(msg.reply_to, "error", {
                                message = "API error: " .. response.status_code .. " - " .. body_text
                            })
                        end
                        return
                    end

                    -- Parse response
                    local result, parse_err = json.decode(response.body)
                    if parse_err then
                        if msg.reply_to then
                            process.send(msg.reply_to, "error", {
                                message = "Failed to parse API response: " .. parse_err
                            })
                        end
                        return
                    end

                    -- Add the response to session history
                    table.insert(session.messages, {
                        role = result.role,
                        content = result.content
                    })

                    -- Send response to caller
                    if msg.reply_to then
                        process.send(msg.reply_to, "response", result)
                    end
                end
            end)
        end,

        -- Submit tool results
        submit_tool_result = function(state, msg)
            local session_id = msg.session_id
            local session = state.active_sessions[session_id]

            if not session then
                if msg.reply_to then
                    process.send(msg.reply_to, "error", {
                        message = "Session not found: " .. tostring(session_id)
                    })
                end
                return
            end

            -- Update last active timestamp
            session.last_active = time.now()

            -- Create tool result content
            local content = {}
            if msg.error then
                content = {
                    {
                        type = "tool_result",
                        tool_use = {  -- Nested structure required by API
                            id = msg.tool_use_id
                        },
                        content = msg.error,
                        status = "error"
                    }
                }
            else
                content = {
                    {
                        type = "tool_result",
                        tool_use = {  -- Nested structure required by API
                            id = msg.tool_use_id
                        },
                        content = msg.result
                    }
                }
            end

            -- Add the user message with tool result
            table.insert(session.messages, {
                role = "user",
                content = content
            })

            -- Make API call to continue the conversation
            coroutine.spawn(function()
                local headers = {
                    ["Content-Type"] = "application/json",
                    ["x-api-key"] = session.api_key,
                    ["anthropic-version"] = "2023-06-01"
                }

                local claude_request = {
                    model = session.config.model or "claude-3-7-sonnet-20250219",
                    max_tokens = session.config.max_tokens or 4096,
                    temperature = session.config.temperature or 0.7,
                    messages = session.messages
                }

                -- Add system prompt if configured
                if session.config.system then
                    claude_request.system = session.config.system
                end

                -- Add tools if available
                if #session.tools > 0 then
                    claude_request.tools = session.tools
                end

                -- Add streaming parameter if requested
                if msg.stream and msg.reply_to then
                    claude_request.stream = true
                end

                local api_url = "https://api.anthropic.com/v1/messages"

                -- Check if streaming is enabled
                if msg.stream and msg.reply_to then
                    local response, err = http_client.post(api_url, {
                        headers = headers,
                        body = json.encode(claude_request),
                        stream = { buffer_size = 4096 }
                    })

                    if err then
                        process.send(msg.reply_to, "error", {
                            message = "API request failed: " .. err
                        })
                        return
                    end

                    if response.status_code < 200 or response.status_code >= 300 then
                        -- Guard against nil response.body with empty string fallback
                        local body_text = response.body or ""
                        process.send(msg.reply_to, "error", {
                            message = "API error: " .. response.status_code .. " - " .. body_text
                        })
                        return
                    end

                    -- Process streaming response
                    process_streaming_response(msg.reply_to, response.stream, session)
                else
                    -- Non-streaming request
                    local response, err = http_client.post(api_url, {
                        headers = headers,
                        body = json.encode(claude_request),
                        timeout = 120
                    })

                    if err then
                        if msg.reply_to then
                            process.send(msg.reply_to, "error", {
                                message = "API request failed: " .. err
                            })
                        end
                        return
                    end

                    if response.status_code < 200 or response.status_code >= 300 then
                        -- Guard against nil response.body with empty string fallback
                        local body_text = response.body or ""
                        if msg.reply_to then
                            process.send(msg.reply_to, "error", {
                                message = "API error: " .. response.status_code .. " - " .. body_text
                            })
                        end
                        return
                    end

                    -- Parse response
                    local result, parse_err = json.decode(response.body)
                    if parse_err then
                        if msg.reply_to then
                            process.send(msg.reply_to, "error", {
                                message = "Failed to parse API response: " .. parse_err
                            })
                        end
                        return
                    end

                    -- Add the response to session history
                    table.insert(session.messages, {
                        role = result.role,
                        content = result.content
                    })

                    -- Send response to caller
                    if msg.reply_to then
                        process.send(msg.reply_to, "response", result)
                    end
                end
            end)
        end,

        -- Register tool for a session
        register_tool = function(state, msg)
            local session_id = msg.session_id
            local session = state.active_sessions[session_id]

            if not session then
                if msg.reply_to then
                    process.send(msg.reply_to, "error", {
                        message = "Session not found: " .. tostring(session_id)
                    })
                end
                return
            end

            -- Add the tool
            table.insert(session.tools, msg.tool)

            -- Report success
            if msg.reply_to then
                process.send(msg.reply_to, "tool_registered", {
                    tool_name = msg.tool.name
                })
            end
        end,

        -- Get session information
        get_session = function(state, msg)
            local session_id = msg.session_id
            local session = state.active_sessions[session_id]

            if not session then
                if msg.reply_to then
                    process.send(msg.reply_to, "error", {
                        message = "Session not found: " .. tostring(session_id)
                    })
                end
                return
            end

            -- Return session info
            if msg.reply_to then
                process.send(msg.reply_to, "session_info", {
                    id = session.id,
                    created_at = session.created_at,
                    last_active = session.last_active,
                    message_count = #session.messages,
                    tool_count = #session.tools
                })
            end
        end,

        -- Clear a session
        clear_session = function(state, msg)
            local session_id = msg.session_id
            local session = state.active_sessions[session_id]

            if not session then
                if msg.reply_to then
                    process.send(msg.reply_to, "error", {
                        message = "Session not found: " .. tostring(session_id)
                    })
                end
                return
            end

            -- Clear session messages
            session.messages = {}

            -- Update last active timestamp
            session.last_active = time.now()

            -- Report success
            if msg.reply_to then
                process.send(msg.reply_to, "session_cleared", {
                    session_id = session_id
                })
            end
        end,

        -- Close a session
        close_session = function(state, msg)
            local session_id = msg.session_id
            local session = state.active_sessions[session_id]

            if not session then
                if msg.reply_to then
                    process.send(msg.reply_to, "error", {
                        message = "Session not found: " .. tostring(session_id)
                    })
                end
                return
            end

            -- Remove session
            state.active_sessions[session_id] = nil

            -- Report success
            if msg.reply_to then
                process.send(msg.reply_to, "session_closed", {
                    session_id = session_id
                })
            end

            print("Closed session:", session_id)
        end,

        -- List all sessions (for admin purposes)
        list_sessions = function(state, msg)
            local sessions = {}

            for id, session in pairs(state.active_sessions) do
                table.insert(sessions, {
                    id = id,
                    created_at = session.created_at,
                    last_active = session.last_active,
                    message_count = #session.messages,
                    tool_count = #session.tools
                })
            end

            -- Return session list
            if msg.reply_to then
                process.send(msg.reply_to, "session_list", {
                    sessions = sessions
                })
            end
        end,

        -- Default handler for unknown messages
        __default = function(state, msg, topic)
            print("Unknown message received:", topic)
            print("Payload:", json.encode(msg))

            if msg.reply_to then
                process.send(msg.reply_to, "error", {
                    message = "Unknown message type: " .. topic
                })
            end
        end,

        -- Handle process events
        __on_event = function(state, event)
            if event.event.kind == process.EVENT_CANCEL then
                print("Claude LLM service received cancel signal")
            end
        end,

        -- Handle cancellation
        on_cancel = function(state)
            print("Claude LLM service shutting down")
            return actor.exit({ status = "shutdown" })
        end
    })

    -- Start session cleanup background task
    coroutine.spawn(function()
        local interval = time.parse_duration("1h")
        local max_idle_time = time.parse_duration("24h")

        while true do
            -- Wait for interval
            time.sleep(interval)

            -- Find and remove idle sessions
            local now = time.now()
            local to_remove = {}

            for id, session in pairs(state.active_sessions) do
                local idle_time = now:sub(session.last_active)
                if idle_time > max_idle_time then
                    table.insert(to_remove, id)
                end
            end

            -- Remove idle sessions
            for _, id in ipairs(to_remove) do
                state.active_sessions[id] = nil
                print("Removed idle session:", id)
            end
        end
    end)

    -- Run the service actor
    return llm_service.run()
end

-- Helper function to process streaming responses
function process_streaming_response(target_pid, stream, session)
    local full_response = { content = {} }

    while true do
        local chunk = stream:read()
        if not chunk then
            -- Stream ended
            process.send(target_pid, "done", {
                response = full_response
            })
            break
        end

        for line in chunk:gmatch("[^\r\n]+") do
            if line:sub(1, 6) == "data: " then
                local data = line:sub(7)

                -- Check for stream termination message
                if data == "[DONE]" then
                    process.send(target_pid, "done", {
                        response = full_response
                    })
                    break
                end

                -- Parse the JSON data
                local success, event = pcall(json.decode, data)
                if success and event then
                    -- Store the full response data
                    if event.type == "message_start" then
                        full_response.id = event.message.id
                        full_response.model = event.message.model
                        full_response.role = event.message.role
                        full_response.stop_reason = nil

                        -- Add empty message to session history
                        table.insert(session.messages, {
                            role = event.message.role,
                            content = {}
                        })
                    elseif event.type == "content_block_start" then
                        table.insert(full_response.content, {
                            type = event.content_block.type,
                            text = ""
                        })

                        -- Add content block to session message
                        local last_message = session.messages[#session.messages]
                        table.insert(last_message.content, {
                            type = event.content_block.type,
                            text = ""
                        })
                    elseif event.type == "content_block_delta" then
                        local last_block = full_response.content[#full_response.content]
                        if last_block and last_block.type == "text" then
                            last_block.text = last_block.text .. (event.delta.text or "")

                            -- Update session message
                            local last_message = session.messages[#session.messages]
                            local last_message_block = last_message.content[#last_message.content]
                            if last_message_block and last_message_block.type == "text" then
                                last_message_block.text = last_block.text
                            end

                            process.send(target_pid, "update", {
                                text = event.delta.text,
                                index = #full_response.content,
                                block_type = "text"
                            })
                        elseif event.delta.type == "input_json_delta" then
                            -- Handle JSON deltas for tool use
                            local last_block = full_response.content[#full_response.content]
                            if last_block and last_block.type == "tool_use" then
                                -- Initialize input if needed
                                if not last_block.input then
                                    last_block.input = {}
                                end

                                -- Get the partial JSON
                                local partial_json = event.delta.partial_json or ""

                                -- Try to parse the partial JSON if it looks complete
                                if partial_json:match("^%s*{.*}%s*$") then
                                    local success, parsed = pcall(json.decode, partial_json)
                                    if success and parsed then
                                        -- If we got valid JSON, merge it into input
                                        for k, v in pairs(parsed) do
                                            last_block.input[k] = v
                                        end
                                    end
                                else
                                    -- For incomplete JSON, store it for debugging
                                    last_block._partial_json = (last_block._partial_json or "") .. partial_json
                                end

                                -- Update session message
                                local last_message = session.messages[#session.messages]
                                local last_message_block = last_message.content[#last_message.content]
                                if last_message_block and last_message_block.type == "tool_use" then
                                    last_message_block.input = last_block.input
                                end
                            end
                        end
                    elseif event.type == "message_delta" and event.delta.stop_reason then
                        full_response.stop_reason = event.delta.stop_reason
                    elseif event.type == "tool_use" then
                        -- Validate that we have a tool_use id
                        if not event.tool_use or not event.tool_use.id then
                            process.send(target_pid, "error", {
                                message = "Invalid tool_use event: missing tool_use.id"
                            })
                        else
                            -- Add to full response
                            table.insert(full_response.content, {
                                type = "tool_use",
                                id = event.tool_use.id,
                                name = event.tool_use.name,
                                input = event.tool_use.input
                            })

                            -- Add tool use to session message
                            local last_message = session.messages[#session.messages]
                            table.insert(last_message.content, {
                                type = "tool_use",
                                id = event.tool_use.id,
                                name = event.tool_use.name,
                                input = event.tool_use.input
                            })

                            -- Send to client with debugging information
                            process.send(target_pid, "tool_use", {
                                tool_use_id = event.tool_use.id,
                                name = event.tool_use.name,
                                input = event.tool_use.input,
                                raw_event = json.encode(event)     -- Add raw event for debugging
                            })
                        end
                    end
                end
            end
        end
    end

    -- Clean up the stream
    stream:close()
end

return { run = run }
local time = require("time")
local json = require("json")
local actor = require("actor")
local funcs = require("funcs")
local prompt = require("prompt")

-- Constants
local PING_INTERVAL = "60s"
local UPDATE_INTERVAL = "30s"
local USER_HUB_REGISTRY_PREFIX = "user_hub."
local WS_JOIN_TOPIC = "ws.join"
local WS_LEAVE_TOPIC = "ws.leave"
local WS_MESSAGE_TOPIC = "ws.message"
local WS_CANCEL_TOPIC = "ws.cancel"
local WELCOME_TOPIC = "welcome"
local UPDATE_TOPIC = "update"
local STATS_PING_TOPIC = "stats.ping"
local LLM_RESPONSE_TOPIC = "llm_response"

-- Command prefixes
local CMD_PREFIX = "/"
local CMD_MODEL = "model"
local CMD_PROVIDER = "provider"
local CMD_CLEAR = "clear"
local CMD_HELP = "help"
local CMD_SYSTEM = "system"

-- User Hub Process - Handles WebSocket connections for a specific user
local function run(args)
    -- Verify required arguments
    if not args or not args.user_id then
        print("Error: user_id is required")
        return { error = "Missing required arguments" }
    end

    local user_id = args.user_id
    local user_metadata = args.user_metadata or {}
    local central_hub_pid = args.central_hub_pid
    local llm_model = args.llm_model or "gpt-4o-mini"
    local llm_provider = args.llm_provider or "openai"

    -- Initialize actor state
    local initial_state = {
        user_id = user_id,
        metadata = user_metadata,
        connected_clients = {},
        client_count = 0,
        last_activity = time.now(),
        start_time = time.now(),
        messages_handled = 0,
        ping_ticker = nil,
        update_ticker = nil,
        central_hub_pid = central_hub_pid,
        prompt_builder = prompt.new(),
        llm_model = llm_model,
        llm_provider = llm_provider,
        system_prompt = "You are a helpful AI assistant.",
        active_llm_tasks = {}, -- Track active LLM tasks
        total_tokens = {
            prompt = 0,
            completion = 0,
            thinking = 0,
            total = 0
        }
    }

    -- Define handlers for different message topics
    local handlers = {
        -- Initialize the actor
        __init = function(state)
            -- Register this process with the user's ID for easy discovery
            local registry_name = USER_HUB_REGISTRY_PREFIX .. state.user_id
            process.registry.register(registry_name)
            print("LLM Chat User Hub started for user:", state.user_id, "with PID:", process.pid())

            -- Set process options
            process.set_options({
                trap_links = true
            })

            -- Create ping ticker to send stats to central hub
            state.ping_ticker = time.ticker(PING_INTERVAL)
            state.register_channel(state.ping_ticker:channel(), function(s, _, ok)
                if ok then
                    if s.central_hub_pid then
                        -- Send stats to central hub
                        process.send(s.central_hub_pid, STATS_PING_TOPIC, {
                            user_id = s.user_id,
                            client_count = s.client_count,
                            last_activity = s.last_activity:format_rfc3339(),
                            messages_handled = s.messages_handled,
                            tokens = s.total_tokens
                        })
                    end
                end
                return s
            end)

            -- Create update ticker to send updates to clients
            state.update_ticker = time.ticker(UPDATE_INTERVAL)
            state.register_channel(state.update_ticker:channel(), function(s, _, ok)
                if ok then
                    -- Send update to all connected clients with simplified stats
                    broadcast_to_clients(s, UPDATE_TOPIC, {
                        type = "stats",
                        uptime = tostring(time.now():sub(s.start_time)),
                        clients = s.client_count,
                        messages = s.messages_handled,
                        tokens = s.total_tokens
                    })
                end
                return s
            end)

            -- Initialize prompt builder with system message
            state.prompt_builder:add_system(state.system_prompt)

            return state
        end,

        [LLM_RESPONSE_TOPIC] = function(s, payload)
            -- Add debug logging for every payload received
            if type(payload) == "table" then
                print("Payload type:", payload.type or "nil")
                if payload.type == "content" then print("Content length:", #(payload.content or "")) end
                if payload.type == "thinking" then print("Thinking length:", #(payload.thinking or "")) end
                if payload.type == "done" then print("Done received with meta:", payload.meta ~= nil) end
            end

            -- Check payload type and forward appropriately
            if type(payload) == "table" then
                if payload.type == "content" and payload.content then
                    -- Content chunk
                    broadcast_to_clients(s, UPDATE_TOPIC, {
                        type = "content",
                        content = payload.content
                    })
                elseif payload.type == "thinking" and payload.thinking then
                    -- Thinking output
                    broadcast_to_clients(s, UPDATE_TOPIC, {
                        type = "thinking",
                        content = payload.thinking
                    })
                elseif payload.type == "done" then
                    -- Completion notification
                    broadcast_to_clients(s, UPDATE_TOPIC, {
                        type = "done",
                        model = payload.meta and payload.meta.model or s.llm_model,
                        provider = payload.meta and payload.meta.provider or s.llm_provider
                    })

                    -- Update token statistics if available
                    if payload.meta and payload.meta.usage then
                        local usage = payload.meta.usage
                        s.total_tokens.prompt = s.total_tokens.prompt + (usage.prompt_tokens or 0)
                        s.total_tokens.completion = s.total_tokens.completion + (usage.completion_tokens or 0)

                        -- Check for thinking tokens
                        if usage.completion_tokens_details and usage.completion_tokens_details.reasoning_tokens then
                            s.total_tokens.thinking = s.total_tokens.thinking +
                                usage.completion_tokens_details.reasoning_tokens
                        end

                        s.total_tokens.total = s.total_tokens.prompt + s.total_tokens.completion

                        -- Send token stats update
                        broadcast_to_clients(s, UPDATE_TOPIC, {
                            type = "tokens",
                            session = {
                                prompt = usage.prompt_tokens or 0,
                                completion = usage.completion_tokens or 0,
                                thinking = (usage.completion_tokens_details and usage.completion_tokens_details.reasoning_tokens) or
                                    0,
                                total = (usage.total_tokens or 0)
                            },
                            total = s.total_tokens
                        })
                    end
                elseif payload.type == "error" then
                    -- Error notification
                    broadcast_to_clients(s, UPDATE_TOPIC, {
                        type = "error",
                        error = payload.error,
                        message = payload.message
                    })
                else
                    -- Forward the payload directly for any other types
                    print("Forwarding unrecognized payload type:", payload.type or "nil")
                    broadcast_to_clients(s, UPDATE_TOPIC, {
                        llm_streaming = true,
                        payload = payload
                    })
                end
            else
                -- Just forward anything we don't recognize
                print("Forwarding non-table payload")
                broadcast_to_clients(s, UPDATE_TOPIC, {
                    llm_streaming = true,
                    payload = payload
                })
            end
            return s
        end,

        -- Handle system events
        __on_event = function(state, event)
            return state
        end,

        -- Handle cancellation
        __on_cancel = function(state)
            print("User Hub for", state.user_id, "received cancel request")

            -- Send cancellation notification to all connected clients
            broadcast_to_clients(state, WS_CANCEL_TOPIC, {
                type = "system",
                message = "Hub shutting down"
            })

            -- Stop tickers
            if state.ping_ticker then
                state.ping_ticker:stop()
            end

            if state.update_ticker then
                state.update_ticker:stop()
            end

            return actor.exit({ status = "shutdown", user_id = state.user_id })
        end,

        -- Handle WebSocket join
        [WS_JOIN_TOPIC] = function(state, payload)
            local client_pid = payload.client_pid

            print("Client joined LLM chat hub for", state.user_id, ":", client_pid)

            -- Add client to our list
            state.connected_clients[client_pid] = { connected_on = time.now() }
            state.client_count = state.client_count + 1
            state.last_activity = time.now()

            -- Send welcome message with user ID and current settings
            process.send(client_pid, WELCOME_TOPIC, {
                user_id = state.user_id,
                client_count = state.client_count,
                model = state.llm_model,
                provider = state.llm_provider,
                system_prompt = state.system_prompt,
                tokens = state.total_tokens,
                history_size = state.prompt_builder:get_messages() and #state.prompt_builder:get_messages() or 0
            })

            return state
        end,

        -- Handle WebSocket leave
        [WS_LEAVE_TOPIC] = function(state, payload)
            local client_pid = payload.client_pid

            if state.connected_clients[client_pid] then
                print("Client left LLM chat hub for", state.user_id, ":", client_pid)
                state.connected_clients[client_pid] = nil
                state.client_count = state.client_count - 1
                state.last_activity = time.now()
            end

            return state
        end,

        -- Handle WebSocket messages
        [WS_MESSAGE_TOPIC] = function(state, payload)
            local message_data, err = json.decode(payload)
            if not message_data then
                print("Error decoding message from client", state.user_id, ":", err)
                return state
            end

            -- Update client's last activity time
            state.last_activity = time.now()
            state.messages_handled = state.messages_handled + 1

            -- Extract message text (support both wrapped and direct formats)
            local message_text
            if message_data.data and message_data.data.text then
                message_text = message_data.data.text
            elseif message_data.text then
                message_text = message_data.text
            end

            if not message_text or message_text == "" then
                print("No valid text found in message:", json.encode(message_data))
                return state
            end

            -- Check if this is a command (starts with /)
            if message_text:sub(1, 1) == CMD_PREFIX then
                handle_command(state, message_text)
                return state
            end

            -- Regular message - process with LLM
            -- Add to prompt builder
            state.prompt_builder:add_user(message_text)

            -- Broadcast user message to clients
            broadcast_to_clients(state, UPDATE_TOPIC, {
                type = "user_message",
                content = message_text
            })

            -- Notify clients that LLM processing is starting
            broadcast_to_clients(state, UPDATE_TOPIC, {
                type = "start",
                model = state.llm_model,
                provider = state.llm_provider
            })

            -- Determine which LLM function to call
            local llm_function_path
            if state.llm_provider == "openai" then
                llm_function_path = "wippy.llm.openai:text_generation"
            else
                llm_function_path = "wippy.llm.claude:text_generation"
            end

            -- Create LLM request using the messages from prompt builder
            local messages = state.prompt_builder:get_messages()
            -- Print the messages to debug
            print("Sending", #messages, "messages to LLM")
            for i, msg in ipairs(messages) do
                print("- Message", i, "role:", msg.role)
            end

            local llm_request = {
                model = state.llm_model,
                messages = messages,
                stream = {
                    reply_to = process.pid(),
                    topic = LLM_RESPONSE_TOPIC
                },
                options = {
                    temperature = 0.7,
                    max_tokens = 1024
                },
                timeout = 60
            }

            -- Create a response channel to communicate with the coroutine
            local response_channel = channel.new(1)

            -- Use coroutine.spawn to make the LLM call non-blocking
            print("Spawning coroutine for LLM function:", llm_function_path, "with model:", state.llm_model)
            coroutine.spawn(function()
                -- Call LLM function inside the coroutine
                local result, err = funcs.new():call(llm_function_path, llm_request)

                -- Handle completion
                if err then
                    print("Error calling LLM function:", err)
                    -- Send error directly to clients
                    response_channel:send(err)
                    return
                else
                    print("LLM function call complete, result type:", type(result))
                    if result then
                        print("Result contains result field:", result.result ~= nil)
                        if result.result then
                            -- Add assistant response to conversation history
                            state.prompt_builder:add_assistant(result.result)
                        end
                    end
                end

                -- Close the channel to indicate completion
                response_channel:send(true)
            end)

            -- Register the response channel to be notified when the coroutine finishes
            state.register_channel(response_channel, function(s, value, ok)
                if ok then
                    print("LLM task completed for user:", s.user_id, " with:", tostring(value))
                end
                s.unregister_channel(response_channel)
                return s
            end)

            return state
        end,

        -- Default handler for any other topics
        __default = function(state, topic, payload)
            -- Just forward any other messages to clients
            broadcast_to_clients(state, topic, payload)
            return state
        end
    }

    -- Helper function to handle commands
    function handle_command(state, command_text)
        -- Strip the prefix and split into command and args
        local cmd, args = command_text:match("^" .. CMD_PREFIX .. "(%w+)%s*(.*)")

        if not cmd then
            print("Invalid command format:", command_text)
            return
        end

        if cmd == "art" then
            -- Change model
            broadcast_to_clients(state, UPDATE_TOPIC, {
                type = "artifact",
                artifact_id = "abc",
                artifact_type = "text/markdown",
                content = "## This is a test artifact\n\nThis is a test artifact content"
            })
        elseif cmd == CMD_MODEL then
            -- Change model
            if args and args ~= "" then
                state.llm_model = args
                broadcast_to_clients(state, UPDATE_TOPIC, {
                    type = "system",
                    message = "Model changed to: " .. args
                })
            else
                broadcast_to_clients(state, UPDATE_TOPIC, {
                    type = "system",
                    message = "Current model: " .. state.llm_model
                })
            end
        elseif cmd == CMD_PROVIDER then
            -- Change provider
            if args == "openai" or args == "anthropic" then
                state.llm_provider = args
                broadcast_to_clients(state, UPDATE_TOPIC, {
                    type = "system",
                    message = "Provider changed to: " .. args
                })
            else
                broadcast_to_clients(state, UPDATE_TOPIC, {
                    type = "system",
                    message = "Invalid provider. Use 'openai' or 'anthropic'"
                })
            end
        elseif cmd == CMD_SYSTEM then
            -- Set system prompt
            if args and args ~= "" then
                state.system_prompt = args
                -- Clear and recreate prompt builder with new system message
                state.prompt_builder = prompt.new()
                state.prompt_builder:add_system(state.system_prompt)
                broadcast_to_clients(state, UPDATE_TOPIC, {
                    type = "system",
                    message = "System prompt updated and history cleared"
                })
            else
                broadcast_to_clients(state, UPDATE_TOPIC, {
                    type = "system",
                    message = "Current system prompt: " .. state.system_prompt
                })
            end
        elseif cmd == CMD_CLEAR then
            -- Clear conversation history
            state.prompt_builder = prompt.new()
            state.prompt_builder:add_system(state.system_prompt)
            broadcast_to_clients(state, UPDATE_TOPIC, {
                type = "system",
                message = "Conversation history cleared"
            })
        elseif cmd == CMD_HELP then
            -- Show available commands
            broadcast_to_clients(state, UPDATE_TOPIC, {
                type = "system",
                message = "Available commands:\n" ..
                    "- /" .. CMD_MODEL .. " <model_name> - Change LLM model\n" ..
                    "- /" .. CMD_PROVIDER .. " <openai|anthropic> - Change LLM provider\n" ..
                    "- /" .. CMD_SYSTEM .. " <text> - Set system prompt\n" ..
                    "- /" .. CMD_CLEAR .. " - Clear conversation history\n" ..
                    "- /" .. CMD_HELP .. " - Show this help message"
            })
        else
            -- Unknown command
            broadcast_to_clients(state, UPDATE_TOPIC, {
                type = "system",
                message = "Unknown command: " .. cmd .. ". Type /" .. CMD_HELP .. " for available commands."
            })
        end
    end

    -- Helper function to broadcast a message to all connected clients
    function broadcast_to_clients(state, topic, message)
        -- Add timestamp if not already present
        if not message.time then
            message.time = time.now():format_rfc3339()
        end

        -- Add user ID to every message
        if not message.user_id then
            message.user_id = state.user_id
        end

        for client_pid, _ in pairs(state.connected_clients) do
            process.send(client_pid, topic, message)
        end
    end

    -- Create and run the actor
    local user_hub_actor = actor.new(initial_state, handlers)
    return user_hub_actor.run()
end

return { run = run }

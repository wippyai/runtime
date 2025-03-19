local actor = require("actor")
local time = require("time")
local json = require("json")
local uuid = require("uuid")

-- Import our components
local session_state = require("session_state")
local session_status = require("session_status")
local tool_handler = require("tool_handler")
local agent_manager = require("agent_manager")

-- Constants
local MESSAGE_ACK_TOPIC = "session.message.ack"
local SESSION_MESSAGE_TOPIC = "session.message"
local SESSION_COMMAND_TOPIC = "session.command"
local LLM_RESPONSE_TOPIC = "llm_response"

-- Session Manager Process
local function run(args)
    -- Verify required arguments
    if not args or not args.session_id or not args.user_id then
        print("Error: session_id and user_id are required")
        return { error = "Missing required arguments" }
    end

    local session_id = args.session_id
    local user_id = args.user_id
    local parent_pid = args.parent_pid
    local conn_pid = args.conn_pid
    local primary_context_id = args.primary_context_id
    local start_model = args.start_model or "gpt-4o"
    local start_agent = args.start_agent or "default-agent"
    local kind = args.kind or "default"

    -- Initialize actor state
    local initial_state = {
        session_id = session_id,
        user_id = user_id,
        parent_pid = parent_pid,
        conn_pid = conn_pid,
        start_time = time.now(),
        is_processing = false,
        current_message_id = nil,
        current_model = start_model,
        current_agent = start_agent,
        kind = kind,
        agent_instance = nil,       -- Will hold the agent runner instance
        checkpoint_sequence = 0,    -- Track checkpoint sequence numbers
        active_llm_tasks = {}       -- Track active LLM tasks
    }

    -- Define handlers for different message topics
    local handlers = {
        -- Initialize the actor
        __init = function(state)
            print("Session Manager started for session:", state.session_id, "user:", state.user_id)

            -- Set process options
            process.set_options({
                trap_links = true
            })

            -- Initialize or recover session
            local session, err = session_state.initialize(
                state.session_id,
                state.user_id,
                state.primary_context_id,
                state.kind,
                state.current_model,
                state.current_agent
            )

            if err then
                print("Failed to initialize session:", err)
                return actor.exit({ status = "failed", error = err })
            end

            -- Update state with recovered session information if needed
            if session.recovered then
                state.current_model = session.current_model
                state.current_agent = session.current_agent
            end

            -- Initialize agent
            local success, err = initialize_agent(state)
            if not success then
                print("Failed to initialize agent:", err)
                session_status.send_error(state.conn_pid, state.session_id, "Failed to initialize agent", err)
                return actor.exit({ status = "failed", error = err })
            end

            -- Send initialization status
            session_status.send_status_info(
                state.conn_pid,
                state.session_id,
                "session_started",
                {
                    agent = state.current_agent,
                    model = state.current_model
                }
            )

            -- Create initial checkpoint
            session_state.create_checkpoint(state.session_id, state.checkpoint_sequence, "Session initialized")
            state.checkpoint_sequence = state.checkpoint_sequence + 1

            return state
        end,

        -- Handle incoming user messages
        [SESSION_MESSAGE_TOPIC] = function(state, payload)
            -- Check if we're currently processing a message
            if state.is_processing then
                print("Session", state.session_id, "is busy, rejecting message")
                -- Reject the message since we're already processing one
                session_status.send_message_rejection(state.conn_pid, "Session is processing another message")
                return state
            }

            -- Generate message ID
            local message_id, err = uuid.v7()
            if err then
                print("Failed to generate message UUID:", err)
                session_status.send_message_rejection(state.conn_pid, "Failed to generate message ID")
                return state
            }

            -- Extract data and metadata from payload
            local data, metadata = extract_message_content(payload)

            -- Validate message data
            if not data or (type(data) == "string" and data == "") then
                print("Empty message received for session:", state.session_id)
                session_status.send_message_rejection(state.conn_pid, "Empty message")
                return state
            }

            -- Set processing state
            state.is_processing = true
            state.current_message_id = message_id

            -- Update session status in database
            session_state.update_status(state.session_id, session_state.STATUS.PROCESSING)

            -- Write message to database
            local result, err = session_state.add_user_message(state.session_id, data, metadata)

            if err then
                print("Failed to save message:", err)
                session_status.send_message_rejection(state.conn_pid, "Failed to save message: " .. err)
                state.is_processing = false
                state.current_message_id = nil
                session_state.update_status(state.session_id, session_state.STATUS.IDLE)
                return state
            }

            -- Acknowledge message receipt
            session_status.send_message_ack(state.conn_pid, message_id, "created")

            -- Send processing status
            session_status.send_processing_started(
                state.conn_pid,
                state.session_id,
                message_id,
                state.current_agent,
                state.current_model
            )

            -- Add message to agent
            local success, err = agent_manager.add_user_message(state.agent_instance, data)
            if not success then
                print("Failed to add message to agent:", err)
                session_status.send_error(state.conn_pid, state.session_id, "Failed to add message to agent", err)
                state.is_processing = false
                state.current_message_id = nil
                session_state.update_status(state.session_id, session_state.STATUS.IDLE)
                return state
            }

            -- Create a pending assistant message
            local pending_message, err = session_state.find_or_create_pending_message(
                state.session_id,
                message_id,
                state.current_agent,
                state.current_model
            )

            if err then
                print("Failed to create pending message:", err)
                session_status.send_error(state.conn_pid, state.session_id, "Failed to create pending message", err)
                state.is_processing = false
                state.current_message_id = nil
                session_state.update_status(state.session_id, session_state.STATUS.IDLE)
                return state
            }

            -- Process message with agent
            process_agent_message(state, pending_message.message_id)

            return state
        end,

        -- Handle commands
        [SESSION_COMMAND_TOPIC] = function(state, payload)
            if not payload or not payload.command then
                print("Invalid command received for session:", state.session_id)
                return state
            end

            local command = payload.command

            if command == "change_model" and payload.model then
                handle_model_change(state, payload.model)
            elseif command == "change_agent" and payload.agent then
                handle_agent_change(state, payload.agent)
            elseif command == "update_conn_pid" and payload.conn_pid then
                -- Update connection PID
                state.conn_pid = payload.conn_pid
                print("Updated conn_pid for session", state.session_id, "to", state.conn_pid)
            elseif command == "clear_history" then
                handle_clear_history(state)
            elseif command == "cancel" then
                handle_cancel_processing(state)
            else
                print("Unknown command received:", command)
            end

            return state
        end,

        -- Handle LLM stream responses
        [LLM_RESPONSE_TOPIC] = function(state, payload)
            -- Forward streaming responses to connection process
            if state.current_message_id and state.conn_pid then
                -- Either forward directly or transform as needed
                session_status.forward_llm_stream(
                    state.conn_pid,
                    state.session_id,
                    state.current_message_id,
                    payload
                )

                -- Check if this is the 'done' event
                if type(payload) == "table" and payload.type == "done" then
                    handle_llm_completion(state, payload)
                end
            }

            return state
        end,

        -- Handle system events
        __on_event = function(state, event)
            if event.kind == process.event.LINK_DOWN then
                print("Linked process down for session", state.session_id, ":", event.from)

                -- If this was our parent, we should exit
                if event.from == state.parent_pid then
                    print("Parent process down, shutting down session")
                    return actor.exit({ status = "shutdown", reason = "parent_down" })
                end
            end

            return state
        end,

        -- Handle cancellation
        __on_cancel = function(state, event)
            print("Session", state.session_id, "received cancel request")

            -- Update session status and clean up
            session_state.close(state.session_id, "cancel_requested")

            -- Send cancellation notification
            session_status.send_status_info(
                state.conn_pid,
                state.session_id,
                "session_shutdown",
                { reason = "cancel" }
            )

            return actor.exit({ status = "shutdown", session_id = state.session_id })
        end,

        -- Default handler
        __default = function(state, payload, topic)
            print("Session", state.session_id, "received unhandled message topic:", topic)
            return state
        end
    }

    -- Helper to extract message content
    function extract_message_content(payload)
        local data
        local metadata = {}

        if type(payload) == "table" then
            data = payload.data
            metadata = payload.metadata or {}
        else
            data = payload
        end

        return data, metadata
    end

    -- Helper function to initialize agent
    function initialize_agent(state)
        -- Create agent instance
        local agent_data, err = agent_manager.create(state.current_agent, state.current_model)
        if not agent_data then
            return false, "Failed to create agent: " .. (err or "unknown error")
        }

        -- Store agent instance
        state.agent_instance = agent_data

        -- Load conversation history
        local messages, err = session_state.load_conversation_history(state.session_id)
        if not err and messages then
            -- Add messages to agent
            local success, err = agent_manager.load_history(agent_data.agent, messages)
            if not success then
                return false, "Failed to load conversation history: " .. err
            end
        }

        return true
    end

    -- Handle model change command
    function handle_model_change(state, new_model)
        if state.is_processing then
            print("Cannot change model while processing a message")
            session_status.send_error(
                state.conn_pid,
                state.session_id,
                "Cannot change model",
                "Session is currently processing a message"
            )
            return
        }

        -- Update model
        local old_model = state.current_model
        state.current_model = new_model

        -- Update database
        local result, err = session_state.update_model(state.session_id, new_model)
        if err then
            print("Failed to update model in database:", err)
            session_status.send_error(state.conn_pid, state.session_id, "Failed to update model", err)
            state.current_model = old_model
            return
        }

        -- Update agent if it exists
        if state.agent_instance then
            state.agent_instance.agent.model = new_model
        }

        -- Send status update
        session_status.send_update(
            state.conn_pid,
            state.session_id,
            session_status.TYPE.MODEL_CHANGE,
            {
                from = old_model,
                to = new_model
            }
        )

        print("Model changed from", old_model, "to", new_model, "for session", state.session_id)
    end

    -- Handle agent change command
    function handle_agent_change(state, new_agent)
        if state.is_processing then
            print("Cannot change agent while processing a message")
            session_status.send_error(
                state.conn_pid,
                state.session_id,
                "Cannot change agent",
                "Session is currently processing a message"
            )
            return
        }

        -- Update agent
        local old_agent = state.current_agent
        state.current_agent = new_agent

        -- Update database
        local result, err = session_state.update_agent(state.session_id, new_agent)
        if err then
            print("Failed to update agent in database:", err)
            session_status.send_error(state.conn_pid, state.session_id, "Failed to update agent", err)
            state.current_agent = old_agent
            return
        }

        -- Reinitialize agent
        local success, err = initialize_agent(state)
        if not success then
            print("Failed to initialize new agent:", err)
            session_status.send_error(state.conn_pid, state.session_id, "Failed to initialize new agent", err)
            -- Revert to previous agent
            state.current_agent = old_agent
            initialize_agent(state)
            return
        }

        -- Send status update
        session_status.send_update(
            state.conn_pid,
            state.session_id,
            session_status.TYPE.AGENT_CHANGE,
            {
                from = old_agent,
                to = new_agent
            }
        )

        print("Agent changed from", old_agent, "to", new_agent, "for session", state.session_id)
    end

    -- Handle clear history command
    function handle_clear_history(state)
        if state.is_processing then
            print("Cannot clear history while processing a message")
            session_status.send_error(
                state.conn_pid,
                state.session_id,
                "Cannot clear history",
                "Session is currently processing a message"
            )
            return
        }

        -- Clear agent history
        if state.agent_instance then
            agent_manager.clear_history(state.agent_instance)
        }

        -- Add system message
        session_state.add_system_message(
            state.session_id,
            "Conversation history cleared",
            "info",
            "session.history_cleared"
        )

        -- Create new checkpoint
        session_state.create_checkpoint(state.session_id, state.checkpoint_sequence, "History cleared")
        state.checkpoint_sequence = state.checkpoint_sequence + 1

        -- Send status update
        session_status.send_status_info(
            state.conn_pid,
            state.session_id,
            "history_cleared",
            {}
        )

        print("Cleared conversation history for session", state.session_id)
    end

    -- Handle cancel processing command
    function handle_cancel_processing(state)
        if not state.is_processing then
            print("No active processing to cancel for session", state.session_id)
            return
        }

        -- Reset processing state
        state.is_processing = false

        -- Delete pending message if exists
        session_state.cleanup_pending_messages(state.session_id)

        -- Update session status
        session_state.update_status(state.session_id, session_state.STATUS.IDLE)

        -- Add system message
        session_state.add_system_message(
            state.session_id,
            "Processing cancelled",
            "info",
            "session.processing_cancelled"
        )

        -- Send update
        session_status.send_processing_cancelled(
            state.conn_pid,
            state.session_id,
            "User cancelled processing"
        )

        print("Cancelled processing for session", state.session_id)

        -- Reset state
        state.current_message_id = nil
    end

    -- Process a message using the agent runner
    function process_agent_message(state, pending_message_id)
        if not state.agent_instance then
            print("Error: No agent initialized for session", state.session_id)
            state.is_processing = false
            session_state.update_status(state.session_id, session_state.STATUS.IDLE)
            session_status.send_error(state.conn_pid, state.session_id, "No agent initialized", nil)
            return
        }

        -- Create a callback function to handle async agent response
        local response_channel = channel.new(1)

        -- Use coroutine.spawn to make the agent call non-blocking
        print("Starting agent processing for session", state.session_id)

        coroutine.spawn(function()
            -- Execute the agent step
            local result, err = agent_manager.process_message(state.agent_instance)

            if err then
                print("Error executing agent:", err)

                -- Delete the pending message on error
                session_state.delete_message(pending_message_id)

                -- Send error to client
                session_status.send_error(
                    state.conn_pid,
                    state.session_id,
                    "agent_error",
                    "Failed to process message: " .. err
                )

                -- Add system error message
                session_state.add_system_message(
                    state.session_id,
                    "Agent execution failed: " .. err,
                    "error",
                    "agent.execution_failed"
                )

                response_channel:send({ status = "error", error = err })
                return
            end

            -- Handle successful agent step
            if result.tool_calls and #result.tool_calls > 0 then
                -- Agent made tool calls
                handle_tool_calls(state, result, pending_message_id, response_channel)
            elseif result.delegate_target then
                -- Agent wants to delegate
                handle_delegation(state, result, pending_message_id, response_channel)
            else
                -- Normal assistant message
                handle_assistant_response(state, result, pending_message_id, response_channel)
            end
        end)

        -- Register the response channel to be notified when the agent processing completes
        state.register_channel(response_channel, function(s, value, ok)
            if ok then
                print("Agent task completed for session:", s.session_id)

                if type(value) == "table" then
                    if value.status == "error" then
                        session_status.send_error(
                            s.conn_pid,
                            s.session_id,
                            "processing_error",
                            value.error
                        )
                    else
                        session_status.send_processing_complete(
                            s.conn_pid,
                            s.session_id,
                            value.tokens
                        )

                        -- Create a new checkpoint after successful processing
                        session_state.create_checkpoint(
                            s.session_id,
                            s.checkpoint_sequence,
                            "Message processed"
                        )
                        s.checkpoint_sequence = s.checkpoint_sequence + 1
                    end
                }

                -- Reset processing state
                s.is_processing = false
                s.current_message_id = nil
                session_state.update_status(s.session_id, session_state.STATUS.IDLE)
            else
                print("Agent channel closed unexpectedly for session:", s.session_id)
                s.is_processing = false
                s.current_message_id = nil
                session_state.update_status(s.session_id, session_state.STATUS.IDLE)
            end

            s.unregister_channel(response_channel)
            return s
        end)
    end

    -- Handle LLM completion event
    function handle_llm_completion(state, payload)
        -- Update token usage if available
        if payload.meta and payload.meta.usage then
            local usage = payload.meta.usage

            -- Record token usage in database
            local result, err = session_state.record_token_usage(
                state.session_id,
                state.current_model,
                {
                    prompt_tokens = usage.prompt_tokens or 0,
                    completion_tokens = usage.completion_tokens or 0,
                    thinking_tokens = (usage.completion_tokens_details and
                                      usage.completion_tokens_details.reasoning_tokens) or 0
                }
            )

            if err then
                print("Failed to record token usage:", err)
            end
        end
    end

    -- Handle tool calls from agent
    function handle_tool_calls(state, result, pending_message_id, response_channel)
        -- Update the pending message with the assistant response
        local update_result, err = session_state.update_assistant_message(
            pending_message_id,
            result.result
        )

        if err then
            print("Failed to update assistant message:", err)
            response_channel:send({ status = "error", error = "Failed to update message" })
            return
        }

        -- Add assistant message to agent conversation
        agent_manager.add_assistant_message(state.agent_instance, result.result)

        -- Context for tool execution
        local tool_context = {
            session_id = state.session_id,
            agent_id = state.current_agent,
            user_id = state.user_id,
            handle_internal_tool = function(tool_name, args)
                -- Handle internal tools like session:change_model
                if tool_name == "session:change_model" and args.model then
                    handle_model_change(state, args.model)
                    return { success = true, model = args.model }
                elseif tool_name == "session:change_agent" and args.agent then
                    handle_agent_change(state, args.agent)
                    return { success = true, agent = args.agent }
                end
                return nil, "Unsupported internal tool"
            end
        }

        -- Execute tool calls
        local tool_success, tool_results = tool_handler.handle_tool_calls(
            result.tool_calls,
            session_state,
            tool_context
        )

        -- Process each tool call and result
        for _, tool_call in ipairs(result.tool_calls) do
            -- Add tool call message to database
            local tool_call_result, err = session_state.add_tool_call(
                state.session_id,
                tool_call.name,
                tool_call.name,  -- Use name as ID for simplicity
                tool_call.arguments,
                tool_call.id,
                state.current_agent
            )

            if err then
                print("Failed to save tool call:", err)
                continue
            }

            -- Send tool notification
            session_status.send_tool_update(
                state.conn_pid,
                state.session_id,
                {
                    operation = "call",
                    name = tool_call.name,
                    call_id = tool_call.id,
                    arguments = tool_call.arguments
                }
            )

            -- Add function call to agent
            agent_manager.add_function_call(
                state.agent_instance,
                tool_call.name,
                tool_call.arguments,
                tool_call.id
            )

            -- Get the result for this tool call
            if tool_results and tool_results[tool_call.id] then
                local tool_result = tool_results[tool_call.id]

                -- Add tool result to database
                local result_entry, err = session_state.add_tool_result(
                    state.session_id,
                    tool_call.name,
                    tool_result.result,
                    tool_call.id
                )

                if err then
                    print("Failed to save tool result:", err)
                    continue
                }

                -- Send tool result notification
                session_status.send_tool_update(
                    state.conn_pid,
                    state.session_id,
                    {
                        operation = "result",
                        name = tool_call.name,
                        call_id = tool_call.id,
                        result = tool_result.result,
                        error = tool_result.error
                    }
                )

                -- Add function result to agent
                agent_manager.add_function_result(
                    state.agent_instance,
                    tool_call.name,
                    tool_result.success and tool_result.result or { error = tool_result.error },
                    tool_call.id
                )
            }
        end

        -- Execute another agent step to process tool results
        local follow_up_result, err = agent_manager.process_message(state.agent_instance)

        if err then
            print("Error in follow-up agent step:", err)
            response_channel:send({ status = "error", error = err })
            return
        }

        -- Create a new message for the follow-up response
        local follow_up_message_id, err = uuid.v7()
        if err then
            print("Failed to generate follow-up message ID:", err)
            response_channel:send({ status = "error", error = "Failed to generate message ID" })
            return
        }

        -- Add follow-up message to database
        local follow_up_msg, err = session_state.add_assistant_message(
            state.session_id,
            follow_up_result.result,
            {
                agent_id = state.current_agent,
                model = state.current_model,
                tokens = follow_up_result.tokens
            }
        )

        if err then
            print("Failed to save follow-up message:", err)
            response_channel:send({ status = "error", error = "Failed to save follow-up message" })
            return
        }

        -- Add assistant response to agent
        agent_manager.add_assistant_message(state.agent_instance, follow_up_result.result)

        -- Complete processing
        response_channel:send({
            status = "success",
            tokens = follow_up_result.tokens
        })
    end

    -- Handle delegation from agent
    function handle_delegation(state, result, pending_message_id, response_channel)
        -- Update the pending message with the assistant response
        local update_result, err = session_state.update_assistant_message(
            pending_message_id,
            result.result
        )

        if err then
            print("Failed to update assistant message:", err)
            response_channel:send({ status = "error", error = "Failed to update message" })
            return
        }

        -- Add assistant message to agent conversation
        agent_manager.add_assistant_message(state.agent_instance, result.result)

        -- Handle the delegation
        local delegation_info, err = agent_manager.handle_delegation(
            result,
            state.current_agent,
            state.current_model
        )

        if err then
            print("Failed to handle delegation:", err)
            response_channel:send({ status = "error", error = "Failed to delegate: " .. err })
            return
        }

        -- Update state with new agent
        local old_agent = state.current_agent
        state.current_agent = delegation_info.to_agent
        state.agent_instance = {
            agent = delegation_info.new_agent,
            spec = delegation_info.new_spec
        }

        -- Update database
        local update_result, err = session_state.update_agent(
            state.session_id,
            state.current_agent
        )

        if err then
            print("Failed to update agent in database:", err)
            response_channel:send({ status = "error", error = "Failed to update agent" })
            return
        }

        -- Send delegation status update
        session_status.send_update(
            state.conn_pid,
            state.session_id,
            session_status.TYPE.AGENT_CHANGE,
            {
                from = old_agent,
                to = state.current_agent,
                message = "Delegating to specialized agent",
                delegated = true
            }
        )

        -- Execute the new agent with delegation message
        local delegate_result, err = agent_manager.process_message(state.agent_instance)

        if err then
            print("Error executing delegated agent:", err)
            response_channel:send({ status = "error", error = err })
            return
        }

        -- Create a new message for the delegated agent's response
        local delegate_message_id, err = uuid.v7()
        if err then
            print("Failed to generate delegate message ID:", err)
            response_channel:send({ status = "error", error = "Failed to generate message ID" })
            return
        }

        -- Add delegated response to database
        local delegate_msg, err = session_state.add_assistant_message(
            state.session_id,
            delegate_result.result,
            {
                agent_id = state.current_agent,
                model = state.current_model,
                tokens = delegate_result.tokens
            }
        )

        if err then
            print("Failed to save delegate message:", err)
            response_channel:send({ status = "error", error = "Failed to save delegate message" })
            return
        }

        -- Add assistant response to agent
        agent_manager.add_assistant_message(state.agent_instance, delegate_result.result)

        -- Complete processing
        response_channel:send({
            status = "success",
            tokens = delegate_result.tokens
        })
    end

    -- Handle normal assistant response
    function handle_assistant_response(state, result, pending_message_id, response_channel)
        -- Update the pending message with the actual assistant response
        local update_result, err = session_state.update_assistant_message(
            pending_message_id,
            result.result,
            {
                agent_id = state.current_agent,
                model = state.current_model,
                tokens = result.tokens,
                thinking = result.thinking
            }
        )

        if err then
            print("Failed to update assistant message:", err)
            response_channel:send({ status = "error", error = "Failed to update message" })
            return
        }

        -- Add assistant message to agent conversation
        agent_manager.add_assistant_message(state.agent_instance, result.result)

        -- Complete processing
        response_channel:send({
            status = "success",
            tokens = result.tokens
        })
    end

    -- Missing helper functions for session state
    session_state.add_user_message = function(session_id, data, metadata)
        if not session_id then
            return nil, "Session ID is required"
        end

        return sessions.add_message(
            session_id,
            "user",
            data,
            metadata or {}
        )
    end

    session_state.add_assistant_message = function(session_id, data, metadata)
        if not session_id then
            return nil, "Session ID is required"
        end

        return sessions.add_message(
            session_id,
            "assistant",
            data,
            metadata or {}
        )
    end

    session_state.delete_message = function(message_id)
        if not message_id then
            return nil, "Message ID is required"
        end

        return sessions.delete_message(message_id)
    end

    -- Create and run the actor
    local session_actor = actor.new(initial_state, handlers)
    return session_actor.run()
end

return { run = run }
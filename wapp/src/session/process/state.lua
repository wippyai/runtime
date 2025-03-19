local sessions = require("sessions")
local json = require("json")
local uuid = require("uuid")

-- Session State Manager
-- Handles all database interactions related to session state
local session_state = {}

-- Session status constants (simplified)
session_state.STATUS = {
    IDLE = "idle",         -- Ready to process messages
    PROCESSING = "processing", -- Currently processing a message
    CLOSED = "closed"      -- Session has been terminated
}

-- Initialize a session (create new or recover existing)
function session_state.initialize(session_id, user_id, primary_context_id, kind, start_model, start_agent)
    if not session_id or not user_id then
        return nil, "Session ID and User ID are required"
    end

    -- First try to recover existing session
    local session, err = sessions.get_session(session_id)

    if not err and session then
        -- Session exists, update last activity time
        print("Recovering existing session:", session_id)

        -- Check if session status is "processing" - this indicates a previous crash
        if session.status == session_state.STATUS.PROCESSING then
            -- Reset to idle since we're recovering
            sessions.update_session_metadata(session_id, {
                status = session_state.STATUS.IDLE,
                last_message_date = os.time()
            })

            -- Add recovery message
            session_state.add_system_message(session_id, "Session recovered after interruption", "info", "session.recovered")
        else
            -- Just update last message date
            sessions.update_session_metadata(session_id, {
                last_message_date = os.time()
            })
        end

        -- Check for pending messages to clean up
        session_state.cleanup_pending_messages(session_id)

        return {
            session_id = session_id,
            user_id = session.user_id,
            primary_context_id = session.primary_context_id,
            current_model = session.current_model,
            current_agent = session.current_agent,
            kind = session.kind,
            status = session_state.STATUS.IDLE,
            recovered = true
        }
    elseif err and err:match("Session not found") then
        -- Create new session
        print("Creating new session:", session_id)

        -- Ensure we have a primary context
        if not primary_context_id then
            local context_data = {
                user_id = user_id,
                kind = kind or "default",
                created_at = os.time()
            }

            local context, err = sessions.create_context("session_data", json.encode(context_data))
            if err then
                return nil, "Failed to create context: " .. err
            end
            primary_context_id = context.context_id
        end

        -- Create the session
        local new_session, err = sessions.create_session(
            session_id,
            user_id,
            primary_context_id,
            "", -- Title will be generated later
            kind or "default",
            start_model or "",
            start_agent or ""
        )

        if err then
            return nil, "Failed to create session: " .. err
        end

        -- Add initialization message
        session_state.add_system_message(session_id, "Session initialized", "info", "session.initialized")

        return {
            session_id = session_id,
            user_id = user_id,
            primary_context_id = primary_context_id,
            current_model = start_model or "",
            current_agent = start_agent or "",
            kind = kind or "default",
            status = session_state.STATUS.IDLE,
            recovered = false
        }
    else
        -- Some other error
        return nil, "Failed to access session: " .. (err or "unknown error")
    end
end

-- Get session by ID
function session_state.get(session_id)
    if not session_id then
        return nil, "Session ID is required"
    end

    return sessions.get_session(session_id)
end

-- Create a new session directly (typically used through initialize)
function session_state.create(session_id, user_id, primary_context_id, kind, model, agent)
    if not session_id or not user_id or not primary_context_id then
        return nil, "Session ID, User ID, and Primary Context ID are required"
    end

    kind = kind or "default"
    model = model or ""
    agent = agent or ""

    return sessions.create_session(
        session_id,
        user_id,
        primary_context_id,
        "", -- Empty title initially
        kind,
        model,
        agent
    )
end

-- Update session status
function session_state.update_status(session_id, status)
    if not session_id or not status then
        return nil, "Session ID and status are required"
    end

    -- Validate status
    local valid_statuses = {
        [session_state.STATUS.IDLE] = true,
        [session_state.STATUS.PROCESSING] = true,
        [session_state.STATUS.CLOSED] = true
    }

    if not valid_statuses[status] then
        return nil, "Invalid status: " .. status
    end

    -- Get current session data
    local session, err = sessions.get_session(session_id)
    if err then
        return nil, "Failed to get session: " .. err
    end

    -- Update session metadata
    return sessions.update_session_metadata(session_id, {
        status = status
    })
end

-- Clean up any pending messages from interrupted sessions
function session_state.cleanup_pending_messages(session_id)
    if not session_id then
        return nil, "Session ID is required"
    end

    -- Get latest message to check if it's pending
    local latest_message, err = sessions.get_latest_message(session_id)
    if err or not latest_message then
        return { cleaned = 0 }
    end

    local cleaned = 0

    -- Check for assistant message with empty content (pending response)
    if latest_message.type == "assistant" then
        local data = latest_message.data
        if (type(data) == "string" and data == "") or
           (type(data) == "table" and (not data.data or data.data == "")) then

            print("Cleaning up pending assistant message:", latest_message.message_id)

            -- Delete the pending message
            local result, err = sessions.delete_message(latest_message.message_id)
            if not err then
                cleaned = cleaned + 1
            end
        end
    end

    -- Also check for tool_call messages without corresponding tool_result
    -- In a real implementation, you would need a way to track which tool calls are pending

    return { cleaned = cleaned }
end

-- Update current model
function session_state.update_model(session_id, model)
    if not session_id or not model then
        return nil, "Session ID and model are required"
    end

    -- Get current session data
    local session, err = sessions.get_session(session_id)
    if err then
        return nil, "Failed to get session: " .. err
    end

    -- Prepare model change message
    local old_model = session.current_model

    -- Update session metadata
    local result, err = sessions.update_session_metadata(session_id, {
        current_model = model
    })

    if err then
        return nil, "Failed to update model: " .. err
    end

    -- Add model change message to database
    local message_id, uuid_err = uuid.v7()
    if not uuid_err then
        local model_change_data = {
            from_model = old_model,
            to_model = model,
            message = "Model changed"
        }

        local model_change_meta = {
            from_model_id = old_model,
            to_model_id = model
        }

        sessions.add_message(
            session_id,
            "model_change",
            model_change_data,
            model_change_meta
        )
    end

    return {
        session_id = session_id,
        from = old_model,
        to = model,
        updated = true
    }
end

-- Update current agent
function session_state.update_agent(session_id, agent)
    if not session_id or not agent then
        return nil, "Session ID and agent are required"
    end

    -- Get current session data
    local session, err = sessions.get_session(session_id)
    if err then
        return nil, "Failed to get session: " .. err
    end

    -- Prepare agent change message
    local old_agent = session.current_agent

    -- Update session metadata
    local result, err = sessions.update_session_metadata(session_id, {
        current_agent = agent
    })

    if err then
        return nil, "Failed to update agent: " .. err
    end

    -- Add agent change message to database
    local message_id, uuid_err = uuid.v7()
    if not uuid_err then
        local agent_change_data = {
            from_agent = old_agent,
            to_agent = agent,
            message = "Agent changed"
        }

        local agent_change_meta = {
            from_agent_id = old_agent,
            to_agent_id = agent
        }

        sessions.add_message(
            session_id,
            "agent_change",
            agent_change_data,
            agent_change_meta
        )
    end

    return {
        session_id = session_id,
        from = old_agent,
        to = agent,
        updated = true
    }
end

-- Update session title
function session_state.update_title(session_id, title)
    if not session_id then
        return nil, "Session ID is required"
    end

    title = title or ""

    return sessions.update_session_title(session_id, title)
end

-- Record token usage
function session_state.record_token_usage(session_id, model, tokens)
    if not session_id or not model then
        return nil, "Session ID and model are required"
    end

    tokens = tokens or {
        prompt_tokens = 0,
        completion_tokens = 0,
        thinking_tokens = 0
    }

    return sessions.record_token_usage(session_id, model, tokens)
end

-- Get token usage for a session
function session_state.get_token_usage(session_id)
    if not session_id then
        return nil, "Session ID is required"
    end

    return sessions.get_session_token_totals(session_id)
end

-- Find or create pending assistant message (for streaming)
function session_state.find_or_create_pending_message(session_id, parent_message_id, agent_id, model)
    if not session_id then
        return nil, "Session ID is required"
    end

    -- Get the latest message
    local latest_message, err = sessions.get_latest_message(session_id)

    -- If the latest message is an empty assistant message, use it
    if latest_message and latest_message.type == "assistant" and
       ((type(latest_message.data) == "string" and latest_message.data == "") or
        (type(latest_message.data) == "table" and not latest_message.data.data)) then

        return latest_message
    end

    -- Otherwise, create a new assistant message
    local message_id, err = uuid.v7()
    if err then
        return nil, "Failed to generate message UUID: " .. err
    end

    -- Add empty assistant message as placeholder
    return sessions.add_message(
        session_id,
        "assistant",
        "", -- Empty content initially
        {
            agent_id = agent_id,
            model = model,
            parent_message_id = parent_message_id
        }
    )
end

-- Update assistant message content
function session_state.update_assistant_message(message_id, content, metadata)
    if not message_id then
        return nil, "Message ID is required"
    end

    -- Update message content
    local result, err = sessions.update_message(message_id, content)
    if err then
        return nil, "Failed to update message content: " .. err
    end

    -- Update metadata if provided
    if metadata then
        local meta_result, meta_err = sessions.update_message_metadata(message_id, metadata)
        if meta_err then
            return nil, "Failed to update message metadata: " .. meta_err
        end
    end

    return { updated = true }
end

-- Add tool call message
function session_state.add_tool_call(session_id, tool_name, tool_id, arguments, call_id, agent_id)
    if not session_id or not tool_name or not call_id then
        return nil, "Session ID, tool name, and call ID are required"
    end

    local tool_call_data = {
        tool_name = tool_name,
        description = "Tool call: " .. tool_name
    }

    local tool_call_meta = {
        agent_id = agent_id,
        tool_id = tool_id or tool_name,
        call_id = call_id,
        arguments = arguments
    }

    return sessions.add_message(
        session_id,
        "tool_call",
        tool_call_data,
        tool_call_meta
    )
end

-- Add tool result message
function session_state.add_tool_result(session_id, tool_name, result, parent_call_id)
    if not session_id or not tool_name or not parent_call_id then
        return nil, "Session ID, tool name, and parent call ID are required"
    end

    local tool_result_data = {
        tool_name = tool_name,
        result_summary = "Tool result for " .. tool_name,
        parent_call_id = parent_call_id
    }

    local tool_result_meta = {
        tool_id = tool_name,
        result = result
    }

    return sessions.add_message(
        session_id,
        "tool_result",
        tool_result_data,
        tool_result_meta
    )
end

-- Add system message
function session_state.add_system_message(session_id, message, level, code, details)
    if not session_id or not message then
        return nil, "Session ID and message are required"
    end

    level = level or "info"
    code = code or "system.message"

    local system_data = {
        message = message,
        level = level
    }

    local system_meta = {
        code = code,
        details = details
    }

    return sessions.add_message(
        session_id,
        "system",
        system_data,
        system_meta
    )
end

-- Check if session exists
function session_state.exists(session_id)
    if not session_id then
        return false
    end

    local session, err = sessions.get_session(session_id)
    return not err and session ~= nil
end

-- Delete pending messages
function session_state.delete_pending_messages(session_id)
    if not session_id then
        return nil, "Session ID is required"
    end

    -- Get the latest message
    local latest_message, err = sessions.get_latest_message(session_id)
    if err or not latest_message then
        return { deleted = 0 }
    end

    -- Check if it's an empty assistant message
    if latest_message.type == "assistant" and
       ((type(latest_message.data) == "string" and latest_message.data == "") or
        (type(latest_message.data) == "table" and not latest_message.data.data)) then

        -- Delete the message
        local result, err = sessions.delete_message(latest_message.message_id)
        if err then
            return nil, "Failed to delete pending message: " .. err
        end

        return { deleted = 1 }
    end

    return { deleted = 0 }
end

-- Load session conversation history
function session_state.load_conversation_history(session_id)
    if not session_id then
        return nil, "Session ID is required"
    end

    return sessions.get_session_messages(session_id)
end

-- Create a checkpoint for the session
function session_state.create_checkpoint(session_id, sequence_number, summary)
    if not session_id then
        return nil, "Session ID is required"
    end

    sequence_number = sequence_number or 1

    -- Get latest message range
    local messages, err = sessions.get_session_messages(session_id)
    if err then
        return nil, "Failed to get messages: " .. err
    end

    -- Get session details
    local session, err = sessions.get_session(session_id)
    if err then
        return nil, "Failed to get session: " .. err
    end

    -- Get token usage
    local tokens, err = sessions.get_session_token_totals(session_id)
    if err then
        tokens = { total = 0 }
    end

    -- Create checkpoint message
    local checkpoint_id, uuid_err = uuid.v7()
    if uuid_err then
        return nil, "Failed to generate UUID: " .. uuid_err
    end

    local first_msg_id = messages[1] and messages[1].message_id or nil
    local last_msg_id = messages[#messages] and messages[#messages].message_id or nil

    local checkpoint_data = {
        checkpoint_type = "auto",
        sequence = sequence_number,
        message = "Conversation checkpoint created"
    }

    local checkpoint_meta = {
        checkpoint_id = checkpoint_id,
        message_range = {
            first_msg_id = first_msg_id,
            last_msg_id = last_msg_id,
            count = #messages
        },
        summary = summary or nil,
        state = {
            agent_id = session.current_agent,
            model = session.current_model,
            tokens_used = tokens.total or 0
        }
    }

    -- Add checkpoint message to database
    local result, err = sessions.add_message(
        session_id,
        "checkpoint",
        checkpoint_data,
        checkpoint_meta
    )

    if err then
        return nil, "Failed to create checkpoint: " .. err
    end

    return {
        checkpoint_id = checkpoint_id,
        sequence = sequence_number,
        message_count = #messages
    }
end

-- Close a session (mark as closed)
function session_state.close(session_id, reason)
    if not session_id then
        return nil, "Session ID is required"
    end

    -- First clean up any pending messages
    session_state.cleanup_pending_messages(session_id)

    -- Update status to closed
    local result, err = session_state.update_status(session_id, session_state.STATUS.CLOSED)
    if err then
        return nil, "Failed to close session: " .. err
    end

    -- Add system message about closure
    local message = "Session closed"
    if reason then
        message = message .. ": " .. reason
    end

    session_state.add_system_message(
        session_id,
        message,
        "info",
        "session.closed",
        { reason = reason }
    )

    -- Create final checkpoint
    session_state.create_checkpoint(session_id, 0, "Session closed")

    return { closed = true }
end

return session_state
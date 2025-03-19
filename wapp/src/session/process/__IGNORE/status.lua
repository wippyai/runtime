local time = require("time")

-- Session Status Updates Manager
-- Handles all status updates and streaming messages to the connection process
local session_status = {}

-- Status update types (simplified)
session_status.TYPE = {
    AGENT_CHANGE = "agent_change", -- Agent has changed
    MODEL_CHANGE = "model_change", -- Model has changed
    PROCESSING = "processing",     -- Message processing has started
    COMPLETED = "completed",       -- Message processing has completed
    ERROR = "error",               -- An error occurred
    STATUS = "status",             -- General status update
    TOOL = "tool"                  -- Tool operation
}

-- Message stream types
session_status.STREAM_TYPE = {
    CONTENT = "content",
    THINKING = "thinking",
    DONE = "done",
    ERROR = "error"
}

-- Base topic for session status updates
local SESSION_STATUS_TOPIC = "session.status"
local MESSAGE_ACK_TOPIC = "session.message.ack"

-- Send a status update to the connection process
function session_status.send_update(conn_pid, session_id, update_type, data)
    if not conn_pid or not session_id or not update_type then
        return false, "Connection PID, session ID, and update type are required"
    end

    local status_topic = SESSION_STATUS_TOPIC
    if session_id then
        status_topic = "session:" .. session_id .. ":status"
    end

    -- Create update message
    local update = {
        type = update_type,
        session_id = session_id,
        timestamp = os.time(),
        time = time.now():format_rfc3339()
    }

    -- Add data fields to update
    if data then
        for k, v in pairs(data) do
            update[k] = v
        end
    end

    -- Send to connection process
    return process.send(conn_pid, status_topic, update)
end

-- Send message acknowledgment
function session_status.send_message_ack(conn_pid, message_id, status, reason)
    if not conn_pid or not message_id then
        return false, "Connection PID and message ID are required"
    end

    local ack = {
        message_id = message_id,
        status = status or "created",
        timestamp = os.time(),
        time = time.now():format_rfc3339()
    }

    if reason then
        ack.reason = reason
    end

    return process.send(conn_pid, MESSAGE_ACK_TOPIC, ack)
end

-- Send message rejection
function session_status.send_message_rejection(conn_pid, reason)
    if not conn_pid then
        return false, "Connection PID is required"
    end

    local rejection = {
        message_id = nil,
        status = "rejected",
        timestamp = os.time(),
        time = time.now():format_rfc3339(),
        reason = reason or "Session is busy"
    }

    return process.send(conn_pid, MESSAGE_ACK_TOPIC, rejection)
end

-- Forward LLM streaming content
function session_status.send_stream_chunk(conn_pid, session_id, message_id, chunk_type, content)
    if not conn_pid or not session_id or not message_id then
        return false, "Connection PID, session ID, and message ID are required"
    end

    -- Validate chunk type
    local valid_types = {
        [session_status.STREAM_TYPE.CONTENT] = true,
        [session_status.STREAM_TYPE.THINKING] = true,
        [session_status.STREAM_TYPE.DONE] = true,
        [session_status.STREAM_TYPE.ERROR] = true
    }

    if not valid_types[chunk_type] then
        return false, "Invalid chunk type: " .. tostring(chunk_type)
    end

    -- Create stream topic
    local stream_topic = "session:" .. session_id .. ":" .. message_id

    -- Create chunk message
    local chunk = {
        type = chunk_type,
        timestamp = os.time(),
        time = time.now():format_rfc3339()
    }

    -- Add content based on type
    if chunk_type == session_status.STREAM_TYPE.CONTENT then
        chunk.content = content
    elseif chunk_type == session_status.STREAM_TYPE.THINKING then
        chunk.thinking = content
    elseif chunk_type == session_status.STREAM_TYPE.ERROR then
        chunk.error = content.error or "Unknown error"
        chunk.message = content.message or content.error or "An error occurred"
    end

    -- Send to connection process
    return process.send(conn_pid, stream_topic, chunk)
end

-- Forward LLM streaming response directly
function session_status.forward_llm_stream(conn_pid, session_id, message_id, payload)
    if not conn_pid or not session_id or not message_id or not payload then
        return false, "Connection PID, session ID, message ID, and payload are required"
    end

    local stream_topic = "session:" .. session_id .. ":" .. message_id

    -- Send to connection process
    return process.send(conn_pid, stream_topic, payload)
end

-- Send agent change notification
function session_status.send_agent_change(conn_pid, session_id, from_agent, to_agent, message)
    return session_status.send_update(
        conn_pid,
        session_id,
        session_status.TYPE.AGENT_CHANGE,
        {
            from = from_agent,
            to = to_agent,
            message = message or "Agent changed"
        }
    )
end

-- Send model change notification
function session_status.send_model_change(conn_pid, session_id, from_model, to_model)
    return session_status.send_update(
        conn_pid,
        session_id,
        session_status.TYPE.MODEL_CHANGE,
        {
            from = from_model,
            to = to_model
        }
    )
end

-- Send processing started notification
function session_status.send_processing_started(conn_pid, session_id, message_id, agent, model)
    return session_status.send_update(
        conn_pid,
        session_id,
        session_status.TYPE.PROCESSING,
        {
            message_id = message_id,
            agent = agent,
            model = model,
            state = "started"
        }
    )
end

-- Send processing complete notification
function session_status.send_processing_complete(conn_pid, session_id, tokens)
    return session_status.send_update(
        conn_pid,
        session_id,
        session_status.TYPE.COMPLETED,
        {
            tokens = tokens
        }
    )
end

-- Send processing cancelled notification
function session_status.send_processing_cancelled(conn_pid, session_id, reason)
    return session_status.send_update(
        conn_pid,
        session_id,
        session_status.TYPE.PROCESSING,
        {
            state = "cancelled",
            reason = reason
        }
    )
end

-- Send error notification
function session_status.send_error(conn_pid, session_id, error, details)
    return session_status.send_update(
        conn_pid,
        session_id,
        session_status.TYPE.ERROR,
        {
            error = error,
            details = details
        }
    )
end

-- Send tool notification (call or result)
function session_status.send_tool_update(conn_pid, session_id, tool_info)
    return session_status.send_update(
        conn_pid,
        session_id,
        session_status.TYPE.TOOL,
        tool_info
    )
end

-- Send status update with custom info
function session_status.send_status_info(conn_pid, session_id, info_type, data)
    return session_status.send_update(
        conn_pid,
        session_id,
        session_status.TYPE.STATUS,
        {
            info_type = info_type,
            data = data
        }
    )
end

return session_status

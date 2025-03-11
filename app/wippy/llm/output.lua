local time = require("time")

-- Stream Helper Library - For standardized streaming responses
local stream_helper = {}

---------------------------
-- Chunk Types and Error Types
---------------------------

-- Streaming chunk type identifiers
stream_helper.CHUNK_TYPE = {
    CONTENT = "content",         -- Regular text content
    TOOL_CALL = "tool_call",     -- Tool call request
    TOOL_RESULT = "tool_result", -- Tool execution result
    THINKING = "thinking",       -- Thinking process
    DONE = "done",               -- Completion marker
    ERROR = "error"              -- Error message
}

-- Standard role types for messages
stream_helper.ROLE = {
    SYSTEM = "system",
    USER = "user",
    ASSISTANT = "assistant",
    FUNCTION = "function",
    TOOL = "tool"
}

-- Unified error types
stream_helper.ERROR_TYPE = {
    INVALID_REQUEST = "invalid_request",
    AUTHENTICATION = "authentication_error",
    RATE_LIMIT = "rate_limit_exceeded",
    SERVER_ERROR = "server_error",
    CONTEXT_LENGTH = "context_length_exceeded",
    CONTENT_FILTER = "content_filter"
}

---------------------------
-- Stream Helper Functions
---------------------------

-- Helper function to format timestamps consistently
local function format_timestamp()
    return time.now():format("2006-01-02T15:04:05.000Z07:00")
end

-- Create a standardized error object
function stream_helper.make_error(type, message, code, provider)
    return {
        type = type or stream_helper.ERROR_TYPE.SERVER_ERROR,
        message = message or "Unknown error",
        code = code,
        provider = provider,
        timestamp = format_timestamp()
    }
end

-- Send a streaming chunk to the provided PID
function stream_helper.send_chunk(pid, chunk_type, content, meta)
    if not pid then return false end

    local chunk = {
        type = chunk_type,
        timestamp = format_timestamp(),
        content = content,
        meta = meta or {}
    }

    return process.send(pid, "llm_response", chunk)
end

-- Send a final response to the provided PID
function stream_helper.send_done(pid, meta)
    return stream_helper.send_chunk(pid, stream_helper.CHUNK_TYPE.DONE, nil, meta)
end

-- Send an error response to the provided PID
function stream_helper.send_error(pid, error_obj)
    return stream_helper.send_chunk(pid, stream_helper.CHUNK_TYPE.ERROR, error_obj.message, {
        error = error_obj
    })
end

-- Helper to create a usage information structure
function stream_helper.make_usage_info(prompt_tokens, completion_tokens, total_tokens)
    local usage = {
        prompt_tokens = prompt_tokens or 0,
        completion_tokens = completion_tokens or 0
    }

    usage.total_tokens = total_tokens or (usage.prompt_tokens + usage.completion_tokens)

    return usage
end

-- Format a content chunk
function stream_helper.format_content(text)
    return {
        type = stream_helper.CHUNK_TYPE.CONTENT,
        content = text,
        timestamp = format_timestamp()
    }
end

-- Format a thinking chunk
function stream_helper.format_thinking(content, model, provider)
    return {
        type = stream_helper.CHUNK_TYPE.THINKING,
        content = content,
        model = model,
        provider = provider,
        timestamp = format_timestamp()
    }
end

-- Format a tool call chunk
function stream_helper.format_tool_call(name, arguments, tool_id)
    return {
        type = stream_helper.CHUNK_TYPE.TOOL_CALL,
        name = name,
        arguments = arguments,
        tool_id = tool_id,
        timestamp = format_timestamp()
    }
end

-- Format a tool result chunk
function stream_helper.format_tool_result(tool_id, result, error)
    return {
        type = stream_helper.CHUNK_TYPE.TOOL_RESULT,
        tool_id = tool_id,
        result = result,
        error = error,
        timestamp = format_timestamp()
    }
end

-- Create and return a done chunk with metadata
function stream_helper.format_done(meta)
    return {
        type = stream_helper.CHUNK_TYPE.DONE,
        meta = meta or {},
        timestamp = format_timestamp()
    }
end

return stream_helper

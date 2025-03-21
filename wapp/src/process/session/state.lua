local uuid = require("uuid")
local session_repo = require("session_repo")
local message_repo = require("message_repo")
local json = require("json")

-- Status constants
local STATUS = {
    IDLE = "idle",
    RUNNING = "running",
    ERROR = "error",
    FAILED = "failed"
}

-- Message type constants
local MSG_TYPE = {
    SYSTEM = "system",
    USER = "user",
    ASSISTANT = "assistant",
    DEVELOPER = "developer",
    FUNCTION = "function",
    FUNCTION_CALL = "function_call"
}

-- Function status constants
local FUNC_STATUS = {
    PENDING = "pending",
    SUCCESS = "success",
    ERROR = "error"
}

-- Error message constants
local ERR = {
    EMPTY_MESSAGE = "Message text cannot be empty",
    FAILED_STATUS = "Session is in a failed state and cannot process messages",
    MESSAGE_ID_FAILED = "Failed to generate message ID",
    STORE_MESSAGE_FAILED = "Failed to store message",
    AGENT_NAME_REQUIRED = "Agent name is required",
    MODEL_NAME_REQUIRED = "Model name is required",
    FUNCTION_NAME_REQUIRED = "Function name is required",
    FUNCTION_RESULT_REQUIRED = "Function result is required",
    FUNCTION_CALL_ID_REQUIRED = "Function call ID is required"
}

-- SessionState class
local session_state = {}
session_state.__index = session_state

function session_state.new(loader_state)
    local self = setmetatable({}, session_state)

    -- Basic properties from loader state
    self.session_id = loader_state.session_id
    self.user_id = loader_state.user_id
    self.primary_context_id = loader_state.primary_context_id
    self.status = loader_state.status or STATUS.IDLE
    self.public_meta = loader_state.public_meta or {} -- Initialize public_meta

    -- Meta information
    if loader_state.meta then
        self.agent_name = loader_state.meta.agent
        self.model = loader_state.meta.model
        self.provider = loader_state.meta.provider
        self.kind = loader_state.meta.kind
    end

    -- Timestamps
    self.start_date = loader_state.start_date
    self.last_message_date = loader_state.last_message_date
    self.last_message_id = loader_state.last_message_id

    -- Message counts
    self.total_message_count = 0 -- Will be loaded from DB when needed

    -- Message cache
    self.message_cache = {} -- Stores messages by message_id

    -- Context data
    self.context_data = {
        session_id = self.session_id,
        agent_name = self.agent_name,
        model = self.model
    }

    return self
end

-- Update session status in the database
function session_state:update_session_status(status, error_message)
    self.status = status

    local update_data = {
        status = status
    }

    -- Add error message if provided
    if error_message then
        update_data.error = error_message
    end

    -- Add timestamp if needed
    update_data.last_message_date = os.time()

    -- Update in database
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        update_data
    )

    if err then
        print("Failed to update session status: " .. err)
        return false
    end

    return true
end

-- Update session title
function session_state:update_session_title(title)
    if not title or title == "" then
        return false, "Title cannot be empty"
    end

    -- Update in database
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        { title = title }
    )

    if err then
        print("Failed to update session title: " .. err)
        return false
    end

    return true
end

-- Update session configuration (combined method for agent, model, and public_meta)
function session_state:update_session_config(config)
    if not config or type(config) ~= "table" then
        return nil, "Configuration must be a table"
    end

    local update_data = {}

    -- Handle agent name update
    if config.agent_name then
        self.agent_name = config.agent_name
        self.context_data.agent_name = config.agent_name
        update_data.current_agent = config.agent_name
    end

    -- Handle model update
    if config.model then
        self.model = config.model
        self.context_data.model = config.model
        update_data.current_model = config.model
    end

    -- Handle public_meta update
    if config.public_meta and type(config.public_meta) == "table" then
        self.public_meta = config.public_meta
        update_data.public_meta = config.public_meta
    end

    -- Handle status update if included
    if config.status then
        self.status = config.status
        update_data.status = config.status
    end

    -- If nothing to update, return early
    if next(update_data) == nil then
        return true
    end

    -- Update metadata in database
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        update_data
    )

    if err then
        local error_msg = "Failed to update session configuration: " .. err
        self:update_session_status(STATUS.FAILED, error_msg)
        return nil, error_msg
    end

    return true
end

-- Mark session as failed
function session_state:mark_session_failed(error_message)
    -- Update session status to FAILED in the database only
    self.status = STATUS.FAILED
    self:update_session_status(STATUS.FAILED, error_message)
    return true
end

-- Get message by ID (with minimal caching)
function session_state:get_message(message_id)
    if not message_id then
        return nil, "Message ID is required"
    end

    -- Check cache first
    if self.message_cache[message_id] then
        return self.message_cache[message_id]
    end

    -- If not in cache, fetch from database
    local message, err = message_repo.get(message_id)
    if err then
        return nil, "Failed to get message: " .. err
    end

    -- Add to cache
    if message then
        self.message_cache[message_id] = message
    end

    return message
end

-- Count all messages in the session
function session_state:count_all_messages()
    local count, err = message_repo.count_by_session(self.session_id)
    if err then
        print("Failed to count messages: " .. err)
        return 0
    end

    self.total_message_count = count
    return count
end

-- Add a message to the database
function session_state:add_message(message_type, message_content, metadata)
    -- Generate message ID
    local message_id, err = uuid.v7()
    if err then
        return nil, ERR.MESSAGE_ID_FAILED .. ": " .. err
    end

    -- Default metadata if not provided
    metadata = metadata or {}

    -- Add agent information to metadata
    if not metadata.agent_name and self.agent_name then
        metadata.agent_name = self.agent_name
    end

    if not metadata.model and self.model then
        metadata.model = self.model
    end

    -- Create message in DB
    local msg, err = message_repo.create(
        message_id,
        self.session_id,
        message_type,
        message_content,
        metadata
    )

    if err then
        return nil, ERR.STORE_MESSAGE_FAILED .. ": " .. err
    end

    -- Store in cache (with metadata attached)
    local cached_msg = {
        message_id = message_id,
        session_id = self.session_id,
        date = os.time(),
        type = message_type,
        data = message_content,
        metadata = metadata
    }
    self.message_cache[message_id] = cached_msg

    -- Increment total message count
    self.total_message_count = self.total_message_count + 1

    return message_id
end

-- Update message metadata
function session_state:update_message_metadata(message_id, metadata)
    if not message_id then
        return nil, "Message ID is required"
    end

    if not metadata then
        return nil, "Metadata is required"
    end

    -- Update in database
    local result, err = message_repo.update_metadata(message_id, metadata)
    if err then
        return nil, "Failed to update message metadata: " .. err
    end

    -- Update in cache if present
    if self.message_cache[message_id] then
        self.message_cache[message_id].metadata = metadata
    end

    return true
end

-- Convenience methods for adding different message types

function session_state:add_system_message(content, metadata)
    if not content or content == "" then
        return nil, ERR.EMPTY_MESSAGE
    end
    metadata = metadata or {}
    return self:add_message(MSG_TYPE.SYSTEM, content, metadata)
end

function session_state:add_user_message(content, metadata)
    if not content or content == "" then
        return nil, ERR.EMPTY_MESSAGE
    end
    metadata = metadata or {}
    return self:add_message(MSG_TYPE.USER, content, metadata)
end

function session_state:add_assistant_message(content, metadata)
    if not content or content == "" then
        return nil, ERR.EMPTY_MESSAGE
    end
    metadata = metadata or {}
    return self:add_message(MSG_TYPE.ASSISTANT, content, metadata)
end

function session_state:add_developer_message(content, metadata)
    if not content or content == "" then
        return nil, ERR.EMPTY_MESSAGE
    end
    metadata = metadata or {}
    return self:add_message(MSG_TYPE.DEVELOPER, content, metadata)
end

-- Add a function call with pending status
function session_state:add_function_call(function_name, arguments, metadata)
    if not function_name then
        return nil, ERR.FUNCTION_NAME_REQUIRED
    end

    metadata = metadata or {}
    metadata.function_name = function_name
    metadata.status = FUNC_STATUS.PENDING

    -- Convert arguments to string if it's a table
    if type(arguments) == "table" then
        local encoded, err = json.encode(arguments)
        if err then
            return nil, "Failed to encode arguments: " .. err
        end
        arguments = encoded
    end

    -- The message_id will serve as the function call ID
    return self:add_message(MSG_TYPE.FUNCTION_CALL, arguments, metadata)
end

-- Update function call with result
function session_state:update_function_result(message_id, result, is_error, metadata)
    if not message_id then
        return nil, "Message ID is required"
    end

    if result == nil then
        return nil, ERR.FUNCTION_RESULT_REQUIRED
    end

    -- Get the function call message
    local message = self.message_cache[message_id]

    -- If not found in cache, fetch from database
    if not message then
        local fetched_message, err = self:get_message(message_id)
        if err or not fetched_message then
            return nil, "Function call message not found: " .. message_id
        end
        message = fetched_message
    end

    -- Create a new metadata table for the update
    local updated_metadata = {}
    for k, v in pairs(message.metadata or {}) do
        updated_metadata[k] = v
    end

    -- Add result information
    updated_metadata.result = result
    updated_metadata.status = is_error and FUNC_STATUS.ERROR or FUNC_STATUS.SUCCESS

    -- Add any additional metadata
    if metadata then
        for k, v in pairs(metadata) do
            updated_metadata[k] = v
        end
    end

    -- Update the message metadata
    return self:update_message_metadata(message_id, updated_metadata)
end

-- Load messages with limit (most recent messages)
function session_state:load_messages(limit)
    limit = limit or 50 -- Default to 50 messages

    local messages, err = message_repo.list_by_session(self.session_id, limit)
    if err then
        return nil, "Failed to load messages: " .. err
    end

    -- Update cache with loaded messages
    for _, message in ipairs(messages) do
        self.message_cache[message.message_id] = message
    end

    -- Update total count if we don't have it yet
    if self.total_message_count == 0 then
        self.total_message_count = message_repo.count_by_session(self.session_id) or 0
    end

    return messages
end

return session_state

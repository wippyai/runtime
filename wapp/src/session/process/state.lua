local uuid = require("uuid")
local session_repo = require("session_repo")
local message_repo = require("message_repo")
local prompt = require("prompt")

-- Status constants
local STATUS = {
    IDLE = "idle",
    RUNNING = "running",
    ERROR = "error",
    FAILED = "failed"
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
    FUNCTION_RESULT_REQUIRED = "Function result is required"
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

    -- Conversation state
    self.prompt_builder = prompt.new()
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
    if status == STATUS.RUNNING then
        update_data.last_message_date = os.time()
    end

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

-- Load message history
function session_state:load_history()
    local messages, err = message_repo.list_by_session(self.session_id)
    if err then
        return nil, "Failed to load messages: " .. err
    end

    -- Sort messages by date
    if messages and #messages > 0 then
        table.sort(messages, function(a, b) return a.date < b.date end)

        -- Rebuild conversation history
        for _, msg in ipairs(messages) do
            -- Convert stored text/metadata to the appropriate prompt builder method
            if msg.type == prompt.ROLE.SYSTEM then
                self.prompt_builder:add_system(msg.data)
            elseif msg.type == prompt.ROLE.USER then
                self.prompt_builder:add_user(msg.data)
            elseif msg.type == prompt.ROLE.ASSISTANT then
                self.prompt_builder:add_assistant(msg.data)
            elseif msg.type == prompt.ROLE.DEVELOPER then
                self.prompt_builder:add_developer(msg.data)
            elseif msg.type == prompt.ROLE.FUNCTION then
                -- Function result messages include function name in metadata
                local meta = msg.metadata or {}
                self.prompt_builder:add_function_result(
                    meta.function_name,
                    msg.data,
                    meta.function_call_id
                )
            elseif msg.type == prompt.ROLE.FUNCTION_CALL then
                -- Function call messages have function details in metadata
                local meta = msg.metadata or {}
                self.prompt_builder:add_function_call(
                    meta.function_name,
                    msg.data, -- Contains the arguments
                    meta.function_call_id
                )
            end
            -- Skip other message types as they don't belong in the prompt
        end
    end

    return true
end

-- Update database with failed status (no client notifications)
function session_state:mark_session_failed(error_message)
    -- Update session status to FAILED in the database only
    self.status = STATUS.FAILED
    self:update_session_status(STATUS.FAILED, error_message)
    return true
end

-- Process different message types
function session_state:add_message(message_type, message_content, metadata)
    -- Generate message ID
    local message_id, err = uuid.v7()
    if err then
        return nil, ERR.MESSAGE_ID_FAILED .. ": " .. err
    end

    -- Default metadata if not provided
    metadata = metadata or {}

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

    -- Add to prompt builder based on message type
    if message_type == prompt.ROLE.SYSTEM then
        self.prompt_builder:add_system(message_content)
    elseif message_type == prompt.ROLE.USER then
        self.prompt_builder:add_user(message_content)
    elseif message_type == prompt.ROLE.ASSISTANT then
        self.prompt_builder:add_assistant(message_content)
    elseif message_type == prompt.ROLE.DEVELOPER then
        self.prompt_builder:add_developer(message_content)
    elseif message_type == prompt.ROLE.FUNCTION then
        self.prompt_builder:add_function_result(
            metadata.function_name,
            message_content,
            metadata.function_call_id
        )
    elseif message_type == prompt.ROLE.FUNCTION_CALL then
        self.prompt_builder:add_function_call(
            metadata.function_name,
            message_content, -- Contains the arguments
            metadata.function_call_id
        )
    end

    return message_id
end

-- Add system message
function session_state:add_system_message(content)
    if not content or content == "" then
        return nil, ERR.EMPTY_MESSAGE
    end

    return self:add_message(prompt.ROLE.SYSTEM, content)
end

-- Add developer message
function session_state:add_developer_message(content)
    if not content or content == "" then
        return nil, ERR.EMPTY_MESSAGE
    end

    return self:add_message(prompt.ROLE.DEVELOPER, content)
end

-- Add function result
function session_state:add_function_result(function_name, content, function_call_id)
    if not function_name then
        return nil, ERR.FUNCTION_NAME_REQUIRED
    end

    if not content then
        return nil, ERR.FUNCTION_RESULT_REQUIRED
    end

    local metadata = {
        function_name = function_name,
        function_call_id = function_call_id
    }

    return self:add_message(prompt.ROLE.FUNCTION, content, metadata)
end

-- Add function call
function session_state:add_function_call(function_name, arguments, function_call_id)
    if not function_name then
        return nil, ERR.FUNCTION_NAME_REQUIRED
    end

    local metadata = {
        function_name = function_name,
        function_call_id = function_call_id
    }

    -- Arguments are stored as the message content, typically JSON
    return self:add_message(prompt.ROLE.FUNCTION_CALL, arguments, metadata)
end

-- Set agent configuration in state
function session_state:set_agent_config(agent_name, model)
    if not agent_name then
        return nil, ERR.AGENT_NAME_REQUIRED
    end

    -- Set agent name and model
    self.agent_name = agent_name
    self.model = model or self.model

    -- Update metadata (including agent and model)
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        {
            current_agent = self.agent_name,
            current_model = self.model,
            status = self.status
        }
    )

    if err then
        local error_msg = "Failed to update session: " .. err
        self:update_session_status(STATUS.FAILED, error_msg)
        return nil, error_msg
    end

    return true
end

-- Change to a different agent
function session_state:change_agent(agent_name)
    if not agent_name then
        return nil, ERR.AGENT_NAME_REQUIRED
    end

    -- Update agent name
    self.agent_name = agent_name

    -- Update metadata
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        { current_agent = self.agent_name }
    )

    if err then
        return nil, "Failed to update session: " .. err
    end

    return true
end

-- Change model
function session_state:change_model(model)
    if not model then
        return nil, ERR.MODEL_NAME_REQUIRED
    end

    -- Update model
    self.model = model
    self.context_data.model = model

    -- Update metadata
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        { current_model = model }
    )

    if err then
        return nil, "Failed to update model: " .. err
    end

    return true
end

return session_state

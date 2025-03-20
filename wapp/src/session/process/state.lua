local uuid = require("uuid")
local session_repo = require("session_repo")
local message_repo = require("message_repo")
local agent_registry = require("agent_registry")
local agent_runner = require("agent_runner")
local prompt = require("prompt")

-- Message types for UI updates (not actual message roles)
local UI_MSG_TYPE = {
    CONTENT = "content"
}

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
    NO_AGENT = "No agent configured for this session",
    AGENT_LOAD_FAILED = "Failed to load agent",
    MESSAGE_ID_FAILED = "Failed to generate message ID",
    RESPONSE_ID_FAILED = "Failed to generate response ID",
    STORE_MESSAGE_FAILED = "Failed to store message",
    STORE_RESPONSE_FAILED = "Failed to store response",
    AGENT_NAME_REQUIRED = "Agent name is required",
    MODEL_NAME_REQUIRED = "Model name is required",
    FUNCTION_NAME_REQUIRED = "Function name is required",
    FUNCTION_RESULT_REQUIRED = "Function result is required"
}

-- SessionState class
local session_state = {}
session_state.__index = session_state

function session_state.new(loader_state, updater)
    local self = setmetatable({}, session_state)

    -- Basic properties from loader state
    self.session_id = loader_state.session_id
    self.user_id = loader_state.user_id
    self.primary_context_id = loader_state.primary_context_id
    self.status = loader_state.status or STATUS.IDLE

    -- Store the updater passed by reference from session
    self.updater = updater

    -- Meta information
    if loader_state.meta then
        self.agent_id = loader_state.meta.agent
        self.model = loader_state.meta.model
        self.provider = loader_state.meta.provider
        self.kind = loader_state.meta.kind
    end

    -- Timestamps
    self.start_date = loader_state.start_date
    self.last_message_date = loader_state.last_message_date
    self.last_message_id = loader_state.last_message_id

    -- Conversation state
    self.agent = nil -- Will be lazy-loaded
    self.prompt_builder = prompt.new()
    self.context_data = {
        session_id = self.session_id,
        agent_id = self.agent_id,
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

-- Lazy load the agent when needed
function session_state:_load_agent()
    if not self.agent and self.agent_id then
        -- Get agent spec by name from the agent_id
        local agent_name = self.agent_id -- agent_id is actually the agent name
        local agent_spec, err = agent_registry.get_by_id(agent_name)
        if not agent_spec then
            local error_msg = ERR.AGENT_LOAD_FAILED .. ": " .. (err or "Unknown error")
            self:update_session_status(STATUS.FAILED, error_msg)
            return nil, error_msg
        end

        -- Override the model if specified
        if self.model then
            agent_spec.model = self.model
        end

        -- Create new agent runner from spec
        local agent, err = agent_runner.new(agent_spec)
        if err then
            local error_msg = ERR.AGENT_LOAD_FAILED .. ": " .. err
            self:update_session_status(STATUS.FAILED, error_msg)
            return nil, error_msg
        end

        -- Set the agent
        self.agent = agent
    end

    return self.agent
end

-- Get prompt slice for the agent
function session_state:_get_prompt_slice()
    -- Simply return the prompt builder which contains all history
    -- In future implementations, this could be optimized to only return
    -- a slice of the conversation based on token limits, memory management, etc.
    return self.prompt_builder
end

-- Initialize with agent by name
function session_state:initialize_with_agent_name(agent_name, model)
    local agent_spec, err = agent_registry.get_by_name(agent_name)
    if not agent_spec then
        local error_msg = ERR.AGENT_LOAD_FAILED .. ": " .. (err or "Unknown error")
        self:update_session_status(STATUS.FAILED, error_msg)
        return nil, error_msg
    end

    -- Set agent ID from spec
    self.agent_id = agent_spec.id
    self.model = model or agent_spec.model

    -- Reset the agent instance to force a reload
    self.agent = nil

    -- Update metadata (including agent and model)
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        {
            current_agent = self.agent_id,
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

    -- Reset agent instance
    self.agent = nil

    -- Get agent spec by name
    local agent_spec, err = agent_registry.get_by_name(agent_name)
    if not agent_spec then
        return nil, ERR.AGENT_LOAD_FAILED .. ": " .. (err or "Unknown error")
    end

    -- Update agent ID
    self.agent_id = agent_spec.id

    -- Update metadata
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        { current_agent = self.agent_id }
    )

    if err then
        return nil, "Failed to update session: " .. err
    end

    -- Send notification about agent change using updater
    if self.updater then
        self.updater:update_session({
            agent = agent_name
        })
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

    -- Reset agent instance to force reload with new model
    self.agent = nil

    -- Update metadata
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        { current_model = model }
    )

    if err then
        return nil, "Failed to update model: " .. err
    end

    -- Notify clients about model change using updater
    if self.updater then
        self.updater:update_session({
            model = model
        })
    end

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

-- Process incoming user message
function session_state:process_message(message_data)
    -- Validate
    if not message_data.text or message_data.text == "" then
        return nil, ERR.EMPTY_MESSAGE
    end

    -- Generate message ID
    local message_id = self:add_message(prompt.ROLE.USER, message_data.text, {
        source = "user",
        files = message_data.file_uuids or {}
    })

    if not message_id then
        return nil, ERR.STORE_MESSAGE_FAILED
    end

    -- Notify clients about message reception (message-specific update)
    if self.updater then
        self.updater:message_received(message_id, message_data.text)
    end

    -- Lazy-load agent if needed
    local agent, err = self:_load_agent()
    if err then
        return nil, err
    end

    if not agent then
        return nil, ERR.NO_AGENT
    end

    -- Return message info for execution
    return {
        message_id = message_id,
        text = message_data.text
    }
end

-- Execute agent with the current prompt slice
function session_state:execute_agent(agent_info, stop_requested)
    local message_id = agent_info.message_id
    local message_text = agent_info.text

    -- Get prompt slice for the agent
    local prompt_slice = self:_get_prompt_slice()

    -- Execute agent with the prompt slice
    local stream_options = nil
    if self.updater and self.updater.conn_pid then
        stream_options = {
            reply_to = self.updater.conn_pid,
            topic = "update:" .. self.session_id .. ":" .. message_id
        }
    end

    local result, err = self.agent:step(prompt_slice, stream_options)

    if err then
        return nil, err
    end

    -- Generate response ID
    local response_id, err = uuid.v7()
    if err then
        return nil, ERR.RESPONSE_ID_FAILED
    end

    -- Check if stop was requested during processing
    if stop_requested then
        -- If stop was requested, don't commit tools or store the response
        return {
            message_id = message_id,
            response_id = nil,
            stopped = true
        }
    end

    -- Create assistant message in DB
    local metadata = {
        agent_id = self.agent_id,
        model = self.model,
        tokens = result.tokens
    }

    local resp, err = message_repo.create(
        response_id,
        self.session_id,
        prompt.ROLE.ASSISTANT,
        result.result,
        metadata
    )

    if err then
        return nil, ERR.STORE_RESPONSE_FAILED .. ": " .. err
    end

    -- Update prompt builder with assistant response
    self.prompt_builder:add_assistant(result.result)

    -- Send content and token usage to client (message-specific updates)
    if self.updater then
        self.updater:_send_message_update(message_id, UI_MSG_TYPE.CONTENT, {
            content = result.result
        })

        self.updater:report_tokens(
            message_id,
            result.tokens.prompt_tokens,
            result.tokens.completion_tokens,
            result.tokens.thinking_tokens
        )
    end

    return {
        message_id = message_id,
        response_id = response_id
    }
end

return session_state

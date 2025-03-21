local agent_registry = require("agent_registry")
local agent_runner = require("agent_runner")
local prompt = require("prompt")
local uuid = require("uuid")

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
    FUNCTION_RESULT_REQUIRED = "Function result is required",
    BUSY = "Session is already processing a message"
}

-- Controller class
local controller = {}
controller.__index = controller

-- Command constants
controller.CMD = {
    MESSAGE = "message",
    STOP = "stop",
    MODEL = "model",
    AGENT = "agent"
}

-- Constructor - add callbacks
function controller.new(session_state, upstream, callbacks)
    local self = setmetatable({}, controller)

    -- Store dependencies
    self.state = session_state
    self.upstream = upstream

    -- Store callbacks for session notifications
    self.callbacks = callbacks or {}

    -- Own the agent instances
    self.agent = nil -- Will be lazy-loaded

    -- Track processing state
    self.is_processing = false
    self.stop_requested = false

    return self
end

-- Continue processing after a previous action
function controller:continue(payload)
    -- Placeholder for autonomous agent actions or multi-step processing
    -- Will be expanded in future implementations

    -- For now, just log the continue action
    print("Controller continue action with payload:", require("json").encode(payload or {}))

    -- Example: could handle autonomous agent actions, multi-turn tool calls, etc.

    return true
end

-- Set agent configuration from existing state
function controller:set_agent_config(agent_name, model)
    -- Reset agent instance to force reload
    self.agent = nil

    -- Notify clients about agent/model configuration
    if self.upstream then
        self.upstream:update_session({
            agent = agent_name,
            model = model
        })
    end

    return true
end

-- Lazy load the agent when needed
function controller:_load_agent()
    -- If we already have an agent, return it
    if self.agent then
        return self.agent
    end

    -- Get agent details from state
    local agent_name = self.state.agent_name
    local model = self.state.model

    if not agent_name then
        return nil, ERR.NO_AGENT
    end

    -- Get agent spec by name from the agent registry
    local agent_spec, err = agent_registry.get_by_name(agent_name)
    if not agent_spec then
        local error_msg = ERR.AGENT_LOAD_FAILED .. ": " .. (err or "Unknown error")
        self.state:update_session_status(STATUS.FAILED, error_msg)
        return nil, error_msg
    end

    -- Override the model if specified
    if model then
        agent_spec.model = model
    end

    -- Create new agent runner from spec
    local agent, err = agent_runner.new(agent_spec)
    if err then
        local error_msg = ERR.AGENT_LOAD_FAILED .. ": " .. err
        self.state:update_session_status(STATUS.FAILED, error_msg)
        return nil, error_msg
    end

    -- Store the agent
    self.agent = agent

    return self.agent
end

-- Handle user message - combined process and execute
function controller:handle_message(message_data)
    -- Validate
    if not message_data.text or message_data.text == "" then
        return nil, ERR.EMPTY_MESSAGE
    end

    -- Check session status from state
    if self.state.status == STATUS.FAILED then
        return nil, ERR.FAILED_STATUS
    end

    -- Check if already processing
    if self.is_processing then
        return nil, ERR.BUSY
    end

    -- Mark as processing
    self.is_processing = true
    self.stop_requested = false

    -- Store message in state and get ID
    local message_id = self.state:add_message(prompt.ROLE.USER, message_data.text, {
        source = "user",
        files = message_data.file_uuids or {}
    })

    if not message_id then
        self.is_processing = false
        return nil, ERR.STORE_MESSAGE_FAILED
    end

    -- Notify clients about message reception
    self.upstream:message_received(message_id, message_data.text)

    -- Generate response ID for upcoming response
    local response_id, err = uuid.v7()
    if err then
        self.is_processing = false
        return nil, ERR.RESPONSE_ID_FAILED
    end

    -- Announce the response is starting
    self.upstream:response_beginning(message_id, response_id)

    -- Lazy-load agent
    local agent, err = self:_load_agent()
    if err then
        self.is_processing = false
        self.upstream:message_error(message_id, "AGENT_ERROR", err)
        return nil, err
    end

    -- Get prompt slice for the agent
    local prompt_slice = self.state.prompt_builder

    -- Configure streaming if available
    local stream_options = nil
    if self.upstream.conn_pid then
        stream_options = {
            reply_to = self.upstream.conn_pid,
            topic = self.upstream:get_message_topic(response_id)
        }
    end

    -- Execute agent
    local result, err = agent:step(prompt_slice, stream_options)

    -- Check for errors
    if err then
        self.is_processing = false
        self.upstream:message_error(message_id, "AGENT_ERROR", err)
        return nil, err
    end

    -- Check if stop was requested during processing
    if self.stop_requested then
        self.is_processing = false
        return {
            message_id = message_id,
            response_id = nil,
            stopped = true
        }
    end

    -- Create metadata for the response
    local metadata = {
        agent_name = self.state.agent_name,
        model = self.state.model,
        tokens = result.tokens
    }

    -- Store response in state
    local stored_response_id, err = self.state:add_message(
        prompt.ROLE.ASSISTANT,
        result.result,
        metadata
    )

    if err then
        self.is_processing = false
        return nil, ERR.STORE_RESPONSE_FAILED .. ": " .. err
    end

    -- Send the full content update
    self.upstream:send_message_update(response_id, "content", {
        content = result.result
    })

    -- Handle delegate agent switching if needed
    if result.delegate_target then
        -- Store the response in the prompt builder
        self.state.prompt_builder:add_assistant(result.result)

        -- Switch agent for next request
        local success, change_err = self:change_agent(result.delegate_target)
        if not success then
            -- Log the error but continue with the response
            print("Failed to switch to delegate agent: " .. change_err)
        end
    else
        -- Add to prompt builder if no delegation
        self.state.prompt_builder:add_assistant(result.result)
    end

    -- Reset processing state
    self.is_processing = false

    -- Set results
    result.message_id = message_id
    result.response_id = response_id

    return result
end

-- Change to a different agent
function controller:change_agent(agent_name)
    if not agent_name then
        return nil, ERR.AGENT_NAME_REQUIRED
    end

    -- Get agent spec by name
    local agent_spec, err = agent_registry.get_by_name(agent_name)
    if not agent_spec then
        return nil, ERR.AGENT_LOAD_FAILED .. ": " .. (err or "Unknown error")
    end

    -- Reset agent instance
    self.agent = nil

    -- Update agent name in state
    local success, err = self.state:change_agent(agent_name)
    if not success then
        return nil, err
    end

    -- Notify clients about agent change
    if self.upstream then
        self.upstream:update_session({
            agent = agent_name
        })
    end

    return true
end

-- Change model
function controller:change_model(model)
    if not model then
        return nil, ERR.MODEL_NAME_REQUIRED
    end

    -- Reset agent instance to force reload with new model
    self.agent = nil

    -- Update model in state
    local success, err = self.state:change_model(model)
    if not success then
        return nil, err
    end

    -- Notify clients about model change
    if self.upstream then
        self.upstream:update_session({
            model = model
        })
    end

    return true
end

-- Handle stop command
function controller:handle_stop_command()
    -- Mark stop requested - will be checked during processing
    self.stop_requested = true

    -- If not currently processing, still mark as not processing
    self.is_processing = false

    return true
end

-- Handle command
function controller:handle_command(command, payload)
    if command == controller.CMD.STOP then
        return self:handle_stop_command()
    elseif command == controller.CMD.MODEL then
        if payload.name then
            return self:change_model(payload.name)
        end
        return nil, "Model name required"
    elseif command == controller.CMD.AGENT then
        if payload.name then
            return self:change_agent(payload.name)
        end
        return nil, "Agent name required"
    else
        return nil, "Unsupported command: " .. command
    end
end

function controller:cancel()
    -- nothing for now
end

-- Set agent and optional model
function controller:init(agent_name, model)
    -- Get agent spec
    local agent_spec, err = agent_registry.get_by_name(agent_name)
    if not agent_spec then
        local error_msg = ERR.AGENT_LOAD_FAILED .. ": " .. (err or "Unknown error")
        self.state:update_session_status(STATUS.FAILED, error_msg)
        return nil, error_msg
    end

    -- Update state with agent information
    local success, err = self.state:set_agent_config(agent_name, model)
    if not success then
        return nil, err
    end

    -- Reset agent instance to force reload
    self.agent = nil

    -- Notify clients about agent change
    if self.upstream then
        self.upstream:update_session({
            agent = agent_name,
            model = model
        })
    end

    return true
end

return controller

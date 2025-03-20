local uuid = require("uuid")
local session_repo = require("session_repo")
local message_repo = require("message_repo")
local agent_registry = require("agent_registry")
local agent_runner = require("agent_runner")
local prompt = require("prompt")

-- Constants
local MSG_TYPE = {
    USER = "user",
    ASSISTANT = "assistant",
    SYSTEM = "system",
    THINKING = "thinking",
    CONTENT = "content",
    RESULT = "result",
    DONE = "done"
}

local STATUS = {
    IDLE = "idle",
    RUNNING = "running",
    ERROR = "error",
    FAILED = "failed" -- New status for permanent failures
}

-- SessionState class
local SessionState = {}
SessionState.__index = SessionState

function SessionState.new(loader_state)
    local self = setmetatable({}, SessionState)

    -- Basic properties from loader state
    self.session_id = loader_state.session_id
    self.user_id = loader_state.user_id
    self.primary_context_id = loader_state.primary_context_id
    self.status = loader_state.status or STATUS.IDLE
    self.conn_pid = nil
    self.parent_pid = nil

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

-- Update connection PID
function SessionState:set_conn_pid(conn_pid)
    self.conn_pid = conn_pid
    return self
end

-- Update parent PID
function SessionState:set_parent_pid(parent_pid)
    self.parent_pid = parent_pid
    return self
end

-- Load message history
function SessionState:load_history()
    -- Load conversation history
    local messages, err = message_repo.list_by_session(self.session_id)
    if err then
        return nil, "Failed to load messages: " .. err
    end

    -- Sort messages by date
    if messages and #messages > 0 then
        table.sort(messages, function(a, b) return a.date < b.date end)

        -- Rebuild conversation history
        for _, msg in ipairs(messages) do
            if msg.type == MSG_TYPE.USER then
                self.prompt_builder:add_user(msg.data)
            elseif msg.type == MSG_TYPE.ASSISTANT then
                self.prompt_builder:add_assistant(msg.data)
            end
        end
    end

    return self
end

-- Mark session as permanently failed
function SessionState:mark_session_failed(error_message)
    -- Update session status to FAILED in the database
    self.status = STATUS.FAILED
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        {
            status = STATUS.FAILED,
            error = error_message
        }
    )

    if err then
        print("Failed to mark session as failed: " .. err)
    end

    -- Notify clients about permanent failure
    self:broadcast({
        type = MSG_TYPE.SYSTEM,
        session_id = self.session_id,
        status = STATUS.FAILED,
        message = "Session failed: " .. error_message
    })

    return self
end

-- Lazy load the agent when needed
function SessionState:_load_agent()
    if not self.agent and self.agent_id then
        -- Get agent spec by name from the agent_id
        local agent_name = self.agent_id -- agent_id is actually the agent name
        local agent_spec, err = agent_registry.get_by_id(agent_name)
        if not agent_spec then
            local error_msg = "Failed to load agent spec: " .. (err or "Unknown error")
            self:mark_session_failed(error_msg)
            return nil, error_msg
        end

        -- Override the model if specified
        if self.model then
            agent_spec.model = self.model
        end

        -- Create new agent runner from spec
        local agent, err = agent_runner.new(agent_spec)
        if err then
            local error_msg = "Failed to create agent: " .. err
            self:mark_session_failed(error_msg)
            return nil, error_msg
        end

        -- Set the agent
        self.agent = agent
    end

    return self.agent
end

-- Get prompt slice for the agent
function SessionState:_get_prompt_slice()
    -- Simply return the prompt builder which contains all history
    -- In future implementations, this could be optimized to only return
    -- a slice of the conversation based on token limits, memory management, etc.
    return self.prompt_builder
end

-- Broadcast message to all clients
function SessionState:broadcast(message, topic_suffix)
    -- Only send if we have connection info
    if not self.parent_pid and not self.conn_pid then
        return false
    end

    local topic = "update:" .. self.session_id
    if topic_suffix then
        topic = topic .. ":" .. topic_suffix
    end

    -- Send to parent (which can relay to all connections)
    if self.parent_pid then
        process.send(self.parent_pid, topic, message)
    end

    return true
end

-- Initialize with agent by name
function SessionState:initialize_with_agent_name(agent_name, model)
    local agent_spec, err = agent_registry.get_by_name(agent_name)
    if not agent_spec then
        local error_msg = "Failed to load agent by name: " .. (err or "Unknown error")
        self:mark_session_failed(error_msg)
        return nil, error_msg
    end

    -- Set agent ID from spec
    self.agent_id = agent_spec.id
    self.model = model or agent_spec.model

    -- Reset the agent instance to force a reload
    self.agent = nil

    if err then
        local error_msg = "Failed to update context: " .. err
        self:mark_session_failed(error_msg)
        return nil, error_msg
    end

    -- Update session metadata
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
        self:mark_session_failed(error_msg)
        return nil, error_msg
    end

    -- Send notification
    self:broadcast({
        type = MSG_TYPE.SYSTEM,
        session_id = self.session_id,
        status = self.status,
        message = "Session initialized with agent: " .. agent_name
    })

    return self
end

-- Change to a different agent
function SessionState:change_agent(agent_name)
    if not agent_name then
        return nil, "Agent name is required"
    end

    -- Reset agent instance
    self.agent = nil

    -- Get agent spec by name
    local agent_spec, err = agent_registry.get_by_name(agent_name)
    if not agent_spec then
        return nil, "Failed to load agent: " .. (err or "Unknown error")
    end

    -- Update agent ID
    self.agent_id = agent_spec.id

    if err then
        return nil, "Failed to update context: " .. err
    end

    -- Update session metadata
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        { current_agent = self.agent_id }
    )

    if err then
        return nil, "Failed to update session: " .. err
    end

    -- Send notification
    self:broadcast({
        type = MSG_TYPE.SYSTEM,
        session_id = self.session_id,
        agent = agent_name
    })

    return self
end

-- Change model
function SessionState:change_model(model)
    if not model then
        return nil, "Model name is required"
    end

    -- Update model
    self.model = model
    self.context_data.model = model

    -- Reset agent instance to force reload with new model
    self.agent = nil

    -- Update session metadata
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        { current_model = model }
    )

    if err then
        return nil, "Failed to update model: " .. err
    end

    -- Notify clients
    self:broadcast({
        type = MSG_TYPE.SYSTEM,
        session_id = self.session_id,
        model = model
    })

    return self
end

-- Process incoming message
function SessionState:process_message(message_data)
    -- Validate
    if not message_data.text or message_data.text == "" then
        return nil, "Message text cannot be empty"
    end

    -- Check if session is in failed state
    if self.status == STATUS.FAILED then
        return nil, "Session is in a failed state and cannot process messages"
    end

    -- Check if already processing
    if self.status == STATUS.RUNNING then
        return nil, "Session is already processing a message"
    end

    -- Generate message ID
    local message_id, err = uuid.v7()
    if err then
        return nil, "Failed to generate message ID: " .. err
    end

    -- Update status
    self.status = STATUS.RUNNING
    local update_result, err = session_repo.update_session_meta(
        self.session_id,
        {
            status = STATUS.RUNNING,
            last_message_date = os.time()
        }
    )

    if err then
        self.status = STATUS.IDLE
        return nil, "Failed to update session status: " .. err
    end

    -- Create message in DB
    local metadata = {
        source = "user",
        files = message_data.file_uuids or {}
    }

    local msg, err = message_repo.create(
        message_id,
        self.session_id,
        MSG_TYPE.USER,
        message_data.text,
        metadata
    )

    if err then
        -- Revert status
        self.status = STATUS.IDLE
        session_repo.update_session_meta(self.session_id, { status = STATUS.IDLE })
        return nil, "Failed to store message: " .. err
    end

    -- Add to prompt builder
    self.prompt_builder:add_user(message_data.text)

    -- Notify clients
    self:broadcast({
        type = MSG_TYPE.SYSTEM, -- todo use proper type
        message_id = message_id,
        session_id = self.session_id,
        status = STATUS.RUNNING,
        message = "Message accepted"
    })

    -- Lazy-load agent if needed
    local agent, err = self:_load_agent()
    if err then
        self:_handle_error(message_id, err)
        return nil, err
    end

    if not agent then
        self:_handle_error(message_id, "No agent configured for this session")
        return nil, "No agent configured"
    end

    -- Send thinking notification
    self:broadcast({
        type = MSG_TYPE.THINKING,
        message_id = message_id,
        content = "Thinking..."
    }, message_id)

    -- Return message info for execution
    return {
        message_id = message_id,
        text = message_data.text
    }
end

-- Execute agent with the current prompt slice
function SessionState:execute_agent(agent_info)
    local message_id = agent_info.message_id
    local message_text = agent_info.text

    -- Get prompt slice for the agent
    local prompt_slice = self:_get_prompt_slice()

    -- Execute agent with the prompt slice
    local stream_options = nil
    if self.conn_pid then
        stream_options = {
            reply_to = self.conn_pid,
            topic = "update:" .. self.session_id .. ":" .. message_id
        }
    end

    local result, err = self.agent:step(prompt_slice, stream_options)

    if err then
        self:_handle_error(message_id, err)
        return nil, err
    end

    -- Generate response ID
    local response_id, err = uuid.v7()
    if err then
        self:_handle_error(message_id, "Failed to generate response ID")
        return nil, "Failed to generate response ID"
    end

    ---- Handle tool calls if present
    --if result.tool_calls and #result.tool_calls > 0 then
    --    -- Check if this is a delegation
    --    if result.delegate_target then
    --        -- Store the delegation as a message
    --        local delegation_metadata = {
    --            agent_id = self.agent_id,
    --            model = self.model,
    --            tokens = result.tokens,
    --            delegate_target = result.delegate_target,
    --            delegate_message = result.delegate_message
    --        }
    --
    --        local resp, err = message_repo.create(
    --            response_id,
    --            self.session_id,
    --            "delegation", -- Message type for delegations
    --            result.delegate_message,
    --            delegation_metadata
    --        )
    --
    --        if err then
    --            self:_handle_error(message_id, "Failed to store delegation: " .. err)
    --            return nil, "Failed to store delegation: " .. err
    --        end
    --
    --        -- Broadcast delegation event
    --        self:broadcast({
    --            type = "delegation",
    --            message_id = message_id,
    --            target_agent = result.delegate_target,
    --            message = result.delegate_message
    --        }, message_id)
    --
    --        -- Update session status
    --        self.status = STATUS.IDLE
    --        session_repo.update_session_meta(self.session_id, { status = STATUS.IDLE })
    --
    --        return {
    --            message_id = message_id,
    --            response_id = response_id,
    --            delegation = {
    --                target = result.delegate_target,
    --                message = result.delegate_message
    --            }
    --        }
    --    else
    --        -- Handle regular tool calls (placeholder implementation)
    --        local tool_call_metadata = {
    --            agent_id = self.agent_id,
    --            model = self.model,
    --            tokens = result.tokens,
    --            tool_calls = result.tool_calls
    --        }
    --
    --        local resp, err = message_repo.create(
    --            response_id,
    --            self.session_id,
    --            "tool_call", -- Message type for tool calls
    --            result.result,
    --            tool_call_metadata
    --        )
    --
    --        if err then
    --            self:_handle_error(message_id, "Failed to store tool call: " .. err)
    --            return nil, "Failed to store tool call: " .. err
    --        end
    --
    --        -- Broadcast tool call event
    --        for _, tool_call in ipairs(result.tool_calls) do
    --            self:broadcast({
    --                type = "tool_call",
    --                message_id = message_id,
    --                tool_name = tool_call.name,
    --                arguments = tool_call.arguments,
    --                tool_call_id = tool_call.id
    --            }, message_id)
    --        end
    --
    --        -- Update session status - remain in RUNNING state until tool calls are processed
    --        return {
    --            message_id = message_id,
    --            response_id = response_id,
    --            tool_calls = result.tool_calls
    --        }
    --    end
    --end

    -- todo: result can be empty if agent only doing tool calls
    -- todo: WE MUST return tool ids and args and etc to parent from this method!!

    -- Create assistant message in DB
    local metadata = {
        agent_id = self.agent_id,
        model = self.model,
        tokens = result.tokens
    }

    local resp, err = message_repo.create(
        response_id,
        self.session_id,
        MSG_TYPE.ASSISTANT,
        result.result,
        metadata
    )

    if err then
        self:_handle_error(message_id, "Failed to store response: " .. err)
        return nil, "Failed to store response: " .. err
    end

    -- Update prompt builder with assistant response
    self.prompt_builder:add_assistant(result.result)

    -- Update session status
    self.status = STATUS.IDLE
    session_repo.update_session_meta(self.session_id, { status = STATUS.IDLE })

    print("DONE")

    -- Send content to client
    self:broadcast({
        type = MSG_TYPE.RESULT,
        message_id = response_id,
        content = result.result
    }, message_id)

    -- Send completion notification
    self:broadcast({
        type = MSG_TYPE.DONE,
        message_id = message_id,
        response_id = response_id
    }, message_id)

    self:broadcast({
        type = MSG_TYPE.SYSTEM,
        session_id = self.session_id,
        status = STATUS.IDLE,
        message = "Processing complete"
    })

    return {
        message_id = message_id,
        response_id = response_id
    }
end

-- Handle error during processing
function SessionState:_handle_error(message_id, error_message)
    -- Update status
    self.status = STATUS.IDLE
    session_repo.update_session_meta(self.session_id, { status = STATUS.IDLE })

    -- Notify clients
    self:broadcast({
        type = MSG_TYPE.SYSTEM,
        message_id = message_id,
        session_id = self.session_id,
        status = STATUS.ERROR,
        message = "Error: " .. error_message
    })
end

-- Stop ongoing generation
function SessionState:stop_generation()
    -- TODO: Implementation for stopping generation
    return true
end

return SessionState

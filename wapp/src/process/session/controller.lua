local uuid = require("uuid")
local json = require("json")
local agent_registry = require("agent_registry")
local agent_runner = require("agent_runner")
local tool_caller = require("tool_caller")
local prompt_builder = require("prompt_builder")

-- Status constants
local STATUS = {
    IDLE = "idle",
    RUNNING = "running",
    ERROR = "error",
    FAILED = "failed"
}

-- Command constants
local COMMANDS = {
    MESSAGE = "message",
    STOP = "stop",
    MODEL = "model",
    AGENT = "agent",
    CONTINUE = "continue",
    TOOLS = "tools"
}

-- Error message constants
local ERR = {
    EMPTY_MESSAGE = "Message text cannot be empty",
    FAILED_STATUS = "Session is in a failed state and cannot process messages",
    NO_AGENT = "No agent configured for this session",
    AGENT_LOAD_FAILED = "Failed to load agent",
    MESSAGE_ID_FAILED = "Failed to generate message ID",
    RESPONSE_ID_FAILED = "Failed to generate response ID",
    AGENT_NAME_REQUIRED = "Agent name is required",
    MODEL_NAME_REQUIRED = "Model name is required",
    BUSY = "Session is already processing a message",
    DELEGATION_FAILED = "Failed to delegate to agent"
}

-- Controller class
local controller = {}
controller.__index = controller

-- Constructor
function controller.new(session_state, upstream)
    local self = setmetatable({}, controller)

    -- Store dependencies
    self.state = session_state
    self.upstream = upstream

    -- Create components
    self.tool_caller = tool_caller.new(session_state, upstream)
    self.prompt_builder = prompt_builder.new(session_state)

    -- Own the agent instance
    self.agent = nil -- Will be lazy-loaded

    -- Track processing state
    self.is_processing = false
    self.stop_requested = false

    -- Next payload for continuation (if any)
    self.next_payload = nil

    return self
end

-- Get current agent instance (lazy loading)
function controller:get_agent()
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
        return nil, error_msg
    end

    -- Override the model if specified
    if model then
        agent_spec.model = model
    end

    -- Add tool IDs if available
    if self.state.tool_ids and #self.state.tool_ids > 0 then
        agent_spec.tools = self.state.tool_ids
    end

    -- Create new agent runner from spec
    local agent, err = agent_runner.new(agent_spec)
    if err then
        local error_msg = ERR.AGENT_LOAD_FAILED .. ": " .. err
        return nil, error_msg
    end

    -- Store the agent
    self.agent = agent

    return self.agent
end

-- Process a normal agent response
function controller:process_normal_response(result, message_id, response_id)
    -- Check if we have tool calls to process
    if result.tool_calls and #result.tool_calls > 0 then
        -- Process tool calls using our unified tool_caller
        local success, control_result = self.tool_caller:process_tool_calls(
            result.tool_calls,
            message_id,
            response_id
        )

        if not success then
            return false, "Tool call failed: " .. (control_result or "Unknown error")
        end

        -- Handle special control results (like model change)
        if control_result and control_result.type == "model_change" then
            -- Change the model
            local change_success, change_err = self:change_model(
                control_result.target_model,
                control_result.reason
            )

            if not change_success then
                return false, "Model change failed: " .. change_err
            end
        end

        -- Process any text content from the agent alongside the tool calls
        if result.result and result.result ~= "" then
            -- Store partial response in state
            local metadata = {
                agent_name = self.state.agent_name,
                model = self.state.model,
                tokens = result.tokens,
                has_tool_calls = true
            }

            -- Store response in state
            local stored_response_id, err = self.state:add_assistant_message(
                result.result,
                metadata
            )

            if err then
                return false, "Failed to store response: " .. err
            end

            -- Send the partial content update
            self.upstream:send_message_update(response_id, "content", {
                content = result.result,
                using_tools = true
            })
        end

        -- Get the current agent
        local agent, err = self:get_agent()
        if err then
            return false, "Failed to get agent: " .. err
        end

        -- Get prompt builder with the appropriate window size including tool responses
        local builder, prompt_err = self.prompt_builder:build_prompt()
        if not builder then
            return false, "Failed to build prompt after tool calls: " .. (prompt_err or "Unknown error")
        end

        -- Configure streaming if available
        local stream_options = nil
        if self.upstream.conn_pid then
            stream_options = {
                reply_to = self.upstream.conn_pid,
                topic = self.upstream:get_message_topic(response_id)
            }
        end

        -- Execute agent for next step with tool results
        local next_result, exec_err = agent:step(builder, stream_options)

        -- Check for errors
        if exec_err then
            self.upstream:message_error(message_id, "AGENT_ERROR", exec_err)
            return false, exec_err
        end

        -- Recursively process the next result
        if next_result.delegate_target then
            -- Agent wants to delegate directly - not via a tool
            return self:process_delegation(next_result, message_id, response_id)
        elseif next_result.tool_calls and #next_result.tool_calls > 0 then
            -- Agent is making more tool calls - recursively process
            return self:process_normal_response(next_result, message_id, response_id)
        else
            -- Final response after tool calls
            local metadata = {
                agent_name = self.state.agent_name,
                model = self.state.model,
                tokens = next_result.tokens,
                after_tool_calls = true
            }

            -- Store final response in state
            local stored_response_id, store_err = self.state:add_assistant_message(
                next_result.result,
                metadata
            )

            if store_err then
                return false, "Failed to store response: " .. store_err
            end

            -- Send the final content update
            self.upstream:send_message_update(response_id, "content", {
                content = next_result.result,
                tools_completed = true
            })

            -- Clear any next payload
            self.next_payload = nil

            return true
        end
    end

    -- Check for empty result
    if not result.result or result.result == "" then
        -- Still notify client that response is empty but handled
        self.upstream:send_message_update(response_id, "content", {
            content = "",
            status = "empty_response",
            agent = self.state.agent_name
        })

        -- Clear any next payload
        self.next_payload = nil

        return true
    end

    -- Normal processing for non-empty response
    local metadata = {
        agent_name = self.state.agent_name,
        model = self.state.model,
        tokens = result.tokens
    }

    -- Store response in state
    local stored_response_id, err = self.state:add_assistant_message(
        result.result,
        metadata
    )

    if err then
        return false, "Failed to store response: " .. err
    end

    -- Send the full content update
    self.upstream:send_message_update(response_id, "content", {
        content = result.result
    })

    -- Clear any next payload
    self.next_payload = nil

    return true
end

-- Process agent delegation (direct delegation, not via tool)
function controller:process_delegation(result, message_id, response_id)
    if not result.delegate_target then
        return false, "No delegation target specified"
    end

    local first_agent_response = result.result or ""
    local has_response = first_agent_response and #first_agent_response > 0

    -- If the first agent provided a non-empty response, store it
    if has_response then
        -- Create metadata for the partial response
        local metadata = {
            agent_name = self.state.agent_name,
            model = self.state.model,
            tokens = result.tokens,
            is_delegate_partial = true,
            delegated_to = result.delegate_target
        }

        -- Store partial response in state
        local stored_response_id, err = self.state:add_assistant_message(
            first_agent_response,
            metadata
        )

        if err then
            -- Don't fail the operation, just log the warning
            print("Warning: Failed to store first agent response before delegation: " .. err)
        else
            -- Send the partial content update
            self.upstream:send_message_update(response_id, "content", {
                content = first_agent_response,
                is_partial = true
            })
        end
    end

    -- Generate a new response ID for the delegated agent response
    local delegated_response_id, err = uuid.v7()
    if err then
        return false, "Failed to generate delegated response ID: " .. err
    end

    -- Create delegation record
    local delegation_metadata = {
        system_action = "delegation",
        from_agent = self.state.agent_name,
        to_agent = result.delegate_target,
        reason = result.delegate_reason,
        message = result.delegate_message or "Continuing with specialized agent",
        original_response_id = response_id,
        delegated_response_id = delegated_response_id
    }

    -- Add a delegation record in state
    local delegation_id = self.state:add_message(
        "delegation",
        "Delegation from '" .. self.state.agent_name .. "' to '" .. result.delegate_target .. "'",
        delegation_metadata
    )

    -- Switch agent
    local success, change_err = self:change_agent(
        result.delegate_target,
        result.delegate_reason or "Delegation requested"
    )

    if not success then
        -- Log the error but continue with the response
        print("Failed to switch to delegate agent: " .. change_err)
        return false, "Failed to switch agent: " .. change_err
    end

    -- Create the continuation data
    self.next_payload = {
        type = "continue",
        message_id = message_id,
        response_id = delegated_response_id, -- Use new response_id
        original_response_id = response_id,  -- Keep track of original for reference
        delegation = {
            delegation_id = delegation_id,
            from_agent = delegation_metadata.from_agent,
            to_agent = result.delegate_target,
            message = result.delegate_message,
            had_partial_response = has_response
        }
    }

    -- Announce the beginning of the delegated response
    self.upstream:response_beginning(message_id, delegated_response_id)

    return true
end

-- Change to a different agent
function controller:change_agent(agent_name, reason)
    if not agent_name then
        return false, ERR.AGENT_NAME_REQUIRED
    end

    -- Get agent spec by name to validate it exists
    local agent_spec, err = agent_registry.get_by_name(agent_name)
    if not agent_spec then
        return false, ERR.AGENT_LOAD_FAILED .. ": " .. (err or "Unknown error")
    end

    -- Remember current agent for logging
    local previous_agent = self.state.agent_name

    -- Reset agent instance
    self.agent = nil

    -- Update agent name in state
    local success, err = self.state:set_agent_config(agent_name, self.state.model)
    if not success then
        return false, err
    end

    -- Get the model from agent spec if it has one and current model is empty
    if agent_spec.model and (not self.state.model or self.state.model == "") then
        self:change_model(agent_spec.model, "Automatically selected for " .. agent_name)
    end

    -- Notify clients about agent change
    if self.upstream then
        self.upstream:update_session({
            agent = agent_name
        })
    end

    -- Log the change if previous agent was set
    if previous_agent then
        -- Create system message for agent change
        local metadata = {
            source = "system",
            agent_change = {
                from = previous_agent,
                to = agent_name
            },
            reason = reason
        }

        local message = "Agent changed from " .. previous_agent ..
            " to " .. agent_name ..
            (reason and (": " .. reason) or "")

        -- Add system message
        self.state:add_system_message(message, metadata)
    end

    return true
end

-- Change model
function controller:change_model(model_name, reason)
    if not model_name then
        return false, ERR.MODEL_NAME_REQUIRED
    end

    -- Remember current model for logging
    local previous_model = self.state.model

    -- Reset agent instance to force reload with new model
    self.agent = nil

    -- Update model in state
    local success, err = self.state:set_agent_config(self.state.agent_name, model_name)
    if not success then
        return false, err
    end

    -- Notify clients about model change
    if self.upstream then
        self.upstream:update_session({
            model = model_name
        })
    end

    -- Log the change if previous model was set
    if previous_model and previous_model ~= "" then
        -- Create system message for model change
        local metadata = {
            source = "system",
            model_change = {
                from = previous_model,
                to = model_name
            },
            reason = reason
        }

        local message = "Model changed from " .. previous_model ..
            " to " .. model_name ..
            (reason and (": " .. reason) or "")

        -- Add system message
        self.state:add_system_message(message, metadata)
    end

    return true
end

-- Set tool IDs for the session
function controller:set_tool_ids(tool_ids)
    -- Store tool IDs in state
    self.state.tool_ids = tool_ids

    -- Reset agent instance to force reload with new tools
    self.agent = nil

    -- Notify clients about tool configuration
    if self.upstream then
        self.upstream:update_session({
            tools = tool_ids
        })
    end

    return true
end

-- Handle user message
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
    local message_id = self.state:add_user_message(message_data.text, {
        source = "user",
        files = message_data.file_uuids or {}
    })

    if not message_id then
        self.is_processing = false
        return nil, "Failed to store message"
    end

    -- Notify clients about message reception
    self.upstream:message_received(message_id, message_data.text)

    -- Generate response ID for upcoming response
    local response_id, err = uuid.v7()
    if err then
        self.is_processing = false
        return nil, ERR.RESPONSE_ID_FAILED
    end

    -- Announce the response is beginning
    self.upstream:response_beginning(message_id, response_id)

    -- Get the agent
    local agent, err = self:get_agent()
    if err then
        self.is_processing = false
        self.upstream:message_error(message_id, "AGENT_ERROR", err)
        return nil, err
    end

    -- Build prompt
    local builder, prompt_err = self.prompt_builder:build_prompt()
    if not builder then
        self.is_processing = false
        self.upstream:message_error(message_id, "PROMPT_ERROR", prompt_err or "Failed to build prompt")
        return nil, prompt_err or "Failed to build prompt"
    end

    -- Configure streaming if available
    local stream_options = nil
    if self.upstream.conn_pid then
        stream_options = {
            reply_to = self.upstream.conn_pid,
            topic = self.upstream:get_message_topic(response_id)
        }
    end

    -- Execute agent
    local result, exec_err = agent:step(builder, stream_options)

    -- Check for errors
    if exec_err then
        self.is_processing = false
        self.upstream:message_error(message_id, "AGENT_ERROR", exec_err)
        return nil, exec_err
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

    -- Process different outcomes based on result
    local final_result = {
        message_id = message_id,
        response_id = response_id
    }

    if result.delegate_target then
        -- Agent wants to delegate directly (not via tool)
        local success, err = self:process_delegation(result, message_id, response_id)
        if not success then
            self.is_processing = false
            self.upstream:message_error(message_id, "DELEGATION_ERROR", err or "Delegation failed")
            return nil, err or ERR.DELEGATION_FAILED
        end

        -- Add next_payload info to result
        final_result.has_next_payload = true
    else
        -- Normal response (could include tool calls)
        local success, err = self:process_normal_response(result, message_id, response_id)
        if not success then
            self.is_processing = false
            self.upstream:message_error(message_id, "RESPONSE_ERROR", err or "Failed to process response")
            return nil, err or "Failed to process response"
        end
    end

    -- Reset processing state
    self.is_processing = false

    -- Copy tokens and result text to final result
    final_result.result = result.result
    final_result.tokens = result.tokens
    final_result.tool_calls = result.tool_calls and #result.tool_calls > 0

    return final_result
end

-- Continue processing with the next payload
function controller:continue(payload)
    -- We just need to process whatever is in the payload
    if not payload or not payload.message_id or not payload.response_id then
        return false, "Invalid continuation payload"
    end

    -- Get the original message
    local message_id = payload.message_id
    local response_id = payload.response_id

    local message, err = self.state:get_message(message_id)
    if not message then
        return false, "Failed to get original message: " .. (err or "Unknown error")
    end

    -- Get the agent (should be the new delegated agent)
    local agent, err = self:get_agent()
    if err then
        self.upstream:message_error(message_id, "AGENT_ERROR", err)
        return false, err
    end

    -- Build a prompt with standard window
    local builder, prompt_err = self.prompt_builder:build_prompt()
    if not builder then
        self.upstream:message_error(message_id, "PROMPT_ERROR", prompt_err or "Failed to build prompt")
        return false, prompt_err or "Failed to build prompt"
    end

    -- Configure streaming if available
    local stream_options = nil
    if self.upstream.conn_pid then
        stream_options = {
            reply_to = self.upstream.conn_pid,
            topic = self.upstream:get_message_topic(response_id)
        }
    end

    -- Execute the agent
    local result, exec_err = agent:step(builder, stream_options)

    -- Check for errors
    if exec_err then
        self.upstream:message_error(message_id, "AGENT_ERROR", exec_err)
        return false, exec_err
    end

    -- Process the result
    local final_result = {}

    if result.delegate_target then
        -- Agent wants to delegate again (directly)
        local success, err = self:process_delegation(result, message_id, response_id)
        if not success then
            return false, err or "Delegation failed"
        end

        -- Add delegation info to result
        final_result.has_next_payload = true
    else
        -- Normal response (could include tool calls)
        local success, err = self:process_normal_response(result, message_id, response_id)
        if not success then
            return false, err or "Failed to process response"
        end
    end

    -- Copy tokens and result text to final result
    final_result.message_id = message_id
    final_result.response_id = response_id
    final_result.result = result.result
    final_result.tokens = result.tokens
    final_result.tool_calls = result.tool_calls and #result.tool_calls > 0

    return final_result
end

-- Handle stop command
function controller:handle_stop_command()
    -- Mark stop requested - will be checked during processing
    self.stop_requested = true

    -- Clear any pending next payload
    self.next_payload = nil

    return true
end

-- Handle commands
function controller:handle_command(command, payload)
    if command == COMMANDS.STOP then
        return self:handle_stop_command()
    elseif command == COMMANDS.MODEL then
        if not payload.name then
            return nil, "Model name required"
        end
        return self:change_model(
            payload.name,
            payload.reason or "Changed by user"
        )
    elseif command == COMMANDS.AGENT then
        if not payload.name then
            return nil, "Agent name required"
        end
        return self:change_agent(
            payload.name,
            payload.reason or "Changed by user"
        )
    elseif command == COMMANDS.TOOLS then
        if not payload.tool_ids then
            return nil, "Tool IDs required"
        end
        return self:set_tool_ids(payload.tool_ids)
    else
        return nil, "Unsupported command: " .. command
    end
end

-- Initialize controller with agent and model
function controller:init(agent_name, model)
    -- Change to the specified agent
    local success, err = self:change_agent(
        agent_name,
        "Initial configuration"
    )

    if not success then
        return nil, err
    end

    -- If model is specified, change to it
    if model then
        success, err = self:change_model(
            model,
            "Initial configuration"
        )

        if not success then
            return nil, err
        end
    end

    return true
end

-- Cancel processing
function controller:cancel()
    -- Mark stop requested
    self.stop_requested = true

    -- Reset processing state
    self.is_processing = false

    -- Clear any pending next payload
    self.next_payload = nil

    return true
end

-- Export constants
controller.CMD = COMMANDS
controller.STATUS = STATUS

return controller

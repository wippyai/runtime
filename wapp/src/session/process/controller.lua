local agent_registry = require("agent_registry")
local agent_runner = require("agent_runner")
local prompt = require("prompt")
local uuid = require("uuid")
local json = require("json")
local funcs = require("funcs")
local tool_resolver = require("tools")

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
    BUSY = "Session is already processing a message",
    DELEGATION_FAILED = "Failed to delegate to agent",
    TOOL_CALL_FAILED = "Failed to execute tool call",
    TOOL_NOT_FOUND = "Tool not found"
}

-- Controller class
local controller = {}
controller.__index = controller

-- Command constants
controller.CMD = {
    MESSAGE = "message",
    STOP = "stop",
    MODEL = "model",
    AGENT = "agent",
    CONTINUE = "continue",
    TOOLS = "tools" -- Added for tool configuration
}

-- Constructor
function controller.new(session_state, upstream)
    local self = setmetatable({}, controller)

    -- Store dependencies
    self.state = session_state
    self.upstream = upstream

    -- Own the agent instances
    self.agent = nil -- Will be lazy-loaded

    -- Track processing state
    self.is_processing = false
    self.stop_requested = false

    -- Next payload for continuation (if any)
    self.next_payload = nil

    return self
end

-- Set tool IDs for the agent
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

    -- Lazy-load agent (should be the new delegated agent)
    local agent, err = self:_load_agent()
    if err then
        self.upstream:message_error(message_id, "AGENT_ERROR", err)
        return false, err
    end

    -- Build a prompt with standard window
    local prompt_builder = self:build_prompt()
    if not prompt_builder then
        self.upstream:message_error(message_id, "PROMPT_ERROR", "Failed to build prompt")
        return false, "Failed to build prompt"
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
    local result, err = agent:step(prompt_builder, stream_options)

    -- Check for errors
    if err then
        self.upstream:message_error(message_id, "AGENT_ERROR", err)
        return false, err
    end

    -- Clear next payload after processing
    self.next_payload = nil

    -- Process the response based on its type
    if result.delegate_target then
        -- Agent wants to delegate again
        return self:process_delegation(result, message_id, response_id)
    elseif result.tool_calls and #result.tool_calls > 0 then
        -- Agent wants to make tool calls
        return self:process_normal_response(result, message_id, response_id)
    else
        -- Normal response - this is the final agent's response
        return self:process_normal_response(result, message_id, response_id)
    end
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

    -- Add tool IDs if available
    if self.state.tool_ids and #self.state.tool_ids > 0 then
        agent_spec.tools = self.state.tool_ids
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

-- Create a prompt builder from current conversation state
function controller:build_prompt(message_limit)
    -- Default to 50 messages if not specified
    message_limit = message_limit or 50

    -- Load messages from state
    local messages, err = self.state:load_messages(message_limit)
    if err then
        return nil, "Failed to load messages: " .. err
    end

    -- Sort by date (oldest first)
    if messages then
        table.sort(messages, function(a, b) return a.date < b.date end)
    else
        messages = {}
    end

    -- Create a prompt builder
    local builder = prompt.new()

    -- Process messages to add to prompt
    for i, msg in ipairs(messages) do
        local meta = msg.metadata or {}

        -- Special handling for delegation messages
        if msg.type == "delegation" then
            -- Convert delegation to tool call and result for LLM's benefit
            if meta.from_agent and meta.to_agent then
                -- Stable tool name based on target agent
                local delegate_tool_name = "delegate_to_" .. meta.to_agent

                -- Create arguments from delegation metadata
                local delegate_args = {
                    from = meta.from_agent,
                    message = meta.message or "Continuing with specialized agent"
                }

                -- Use delegation message ID as the function call ID
                local function_call_id = msg.message_id

                -- Add tool call representing the delegation action
                builder:add_function_call(
                    delegate_tool_name,
                    delegate_args,
                    function_call_id
                )

                -- Create result content
                local result_content = {
                    status = "accepted",
                    message = "Delegation accepted by " .. meta.to_agent
                }

                -- Add tool result representing acceptance
                builder:add_function_result(
                    delegate_tool_name,
                    json.encode(result_content),
                    function_call_id
                )
            end
        else
            -- Normal handling for other message types
            if msg.type == "system" then
                builder:add_system(msg.data)
            elseif msg.type == "user" then
                builder:add_user(msg.data)
            elseif msg.type == "assistant" then
                builder:add_assistant(msg.data)
            elseif msg.type == "developer" then
                builder:add_developer(msg.data)
            elseif msg.type == "function" then
                builder:add_function_result(
                    meta.function_name,
                    msg.data,
                    meta.function_call_id
                )
            elseif msg.type == "function_call" then
                local args = msg.data

                -- Try to parse JSON if it's a string
                if type(args) == "string" then
                    local success, parsed = pcall(json.decode, args)
                    if success then
                        args = parsed
                    end
                end

                builder:add_function_call(
                    meta.function_name,
                    args,
                    meta.function_call_id
                )
            end
        end
    end

    return builder
end

-- Process tool calls and continue the conversation
function controller:process_tool_calls(tool_calls, message_id, response_id)
    -- Check if there are any tool calls
    if not tool_calls or #tool_calls == 0 then
        return true, nil
    end

    -- Initialize function executor
    local executor = funcs.new()

    -- Process each tool call sequentially
    for i, tool_call in ipairs(tool_calls) do
        local tool_name = tool_call.name
        local arguments = tool_call.arguments
        local function_call_id = tool_call.id or uuid.v7()

        -- Stream only tool name to client (no IDs, no args)
        self.upstream:send_message_update(response_id, "tool_call", {
            function_name = tool_name
        })

        -- Store function call in session state
        self.state:add_function_call(
            tool_name,
            arguments,
            function_call_id,
            {
                response_id = response_id,
                message_id = message_id
            }
        )

        print(json.encode(tool_call))


        -- todo dynamic tools


        -- Execute the tool function
        local result, err = executor:call(tool_call.registry_id, arguments)

        -- Handle result or error
        if err then
            -- Stream minimal error to client (only tool name)
            self.upstream:send_message_update(response_id, "tool_error", {
                function_name = tool_name,
                error = "Tool execution failed"
            })

            -- Store error result in session state
            self.state:add_function_result(
                tool_name,
                json.encode({ error = err }),
                function_call_id,
                {
                    error = true,
                    response_id = response_id,
                    message_id = message_id
                }
            )
        else
            -- Convert result to string if needed for storage
            local result_content = result
            if type(result) == "table" then
                result_content = json.encode(result)
            elseif type(result) ~= "string" then
                result_content = tostring(result)
            end

            -- Stream minimal success to client (only tool name)
            self.upstream:send_message_update(response_id, "tool_result", {
                function_name = tool_name,
                status = "success"
            })

            -- Store result in session state
            self.state:add_function_result(
                tool_name,
                result_content,
                function_call_id,
                {
                    response_id = response_id,
                    message_id = message_id
                }
            )
        end

        ::continue::
    end

    return true, nil
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

    -- Announce the response is beginning
    self.upstream:response_beginning(message_id, response_id)

    -- Lazy-load agent
    local agent, err = self:_load_agent()
    if err then
        self.is_processing = false
        self.upstream:message_error(message_id, "AGENT_ERROR", err)
        return nil, err
    end

    -- Get prompt builder with the appropriate window size
    local prompt_builder = self:build_prompt()
    if not prompt_builder then
        self.is_processing = false
        self.upstream:message_error(message_id, "PROMPT_ERROR", "Failed to build prompt")
        return nil, "Failed to build prompt"
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
    local result, err = agent:step(prompt_builder, stream_options)

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

    -- Process different outcomes based on result
    local final_result = {
        message_id = message_id,
        response_id = response_id
    }

    if result.delegate_target then
        -- Agent wants to delegate
        local success, err = self:process_delegation(result, message_id, response_id)
        if not success then
            self.is_processing = false
            self.upstream:message_error(message_id, "DELEGATION_ERROR", err or "Delegation failed")
            return nil, err or ERR.DELEGATION_FAILED
        end

        -- Add next_payload info to result
        final_result.has_next_payload = self.next_payload ~= nil
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

-- Process a normal agent response (no delegation)
function controller:process_normal_response(result, message_id, response_id)
    -- Check if we have tool calls to process
    if result.tool_calls and #result.tool_calls > 0 then
        -- Process tool calls
        local success, err = self:process_tool_calls(result.tool_calls, message_id, response_id)

        if not success then
            return false, ERR.TOOL_CALL_FAILED .. ": " .. (err or "Unknown error")
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
                return false, ERR.STORE_RESPONSE_FAILED .. ": " .. err
            end

            -- Send the partial content update (no detailed tool info)
            self.upstream:send_message_update(response_id, "content", {
                content = result.result,
                using_tools = true
            })
        end

        -- Get prompt builder with the appropriate window size including tool responses
        local prompt_builder = self:build_prompt()
        if not prompt_builder then
            return false, "Failed to build prompt after tool calls"
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
        local next_result, err = self.agent:step(prompt_builder, stream_options)

        -- Check for errors
        if err then
            self.upstream:message_error(message_id, "AGENT_ERROR", err)
            return false, err
        end

        -- Recursively process the next result
        if next_result.delegate_target then
            -- Agent wants to delegate
            return self:process_delegation(next_result, message_id, response_id)
        elseif next_result.tool_calls and #next_result.tool_calls > 0 then
            -- Agent is making more tool calls
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
            local stored_response_id, err = self.state:add_assistant_message(
                next_result.result,
                metadata
            )

            if err then
                return false, ERR.STORE_RESPONSE_FAILED .. ": " .. err
            end

            -- Send the final content update (no detailed tool info)
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
        print("Warning: Agent returned empty response, skipping storage")

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
        return false, ERR.STORE_RESPONSE_FAILED .. ": " .. err
    end

    -- Send the full content update
    self.upstream:send_message_update(response_id, "content", {
        content = result.result
    })

    -- Clear any next payload
    self.next_payload = nil

    return true
end

-- Process agent delegation
function controller:process_delegation(result, message_id, response_id)
    if not result.delegate_target then
        return false, "No delegation target specified"
    end

    local first_agent_response = result.result
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
            print("Warning: Failed to store first agent response before delegation: " .. err)
            -- Don't fail the operation, just log the warning
        else
            -- Send the partial content update
            self.upstream:send_message_update(response_id, "content", {
                content = first_agent_response,
                is_partial = true
            })
        end
    end

    -- Create delegation record with all necessary metadata
    local delegation_metadata = {
        system_action = "delegation",
        from_agent = self.state.agent_name,
        to_agent = result.delegate_target,
        reason = result.delegate_reason,
        message = result.delegate_message or "Continuing with specialized agent",
        response_id = response_id
    }

    -- Add a single delegation record in state
    local delegation_id = self.state:add_message(
        "delegation",
        "Delegation from '" .. self.state.agent_name .. "' to '" .. result.delegate_target .. "'",
        delegation_metadata
    )

    -- Switch agent for next request
    local success, change_err = self:change_agent(result.delegate_target)
    if not success then
        -- Log the error but continue with the response
        print("Failed to switch to delegate agent: " .. change_err)
        return false, "Failed to switch agent: " .. change_err
    end

    -- Create the next payload for continuation
    self.next_payload = {
        type = controller.CMD.CONTINUE,
        message_id = message_id,
        response_id = response_id, -- Reuse the same response_id
        delegation = {
            delegation_id = delegation_id,
            from_agent = self.state.agent_name,
            to_agent = result.delegate_target,
            message = result.delegate_message,
            had_partial_response = has_response
        }
    }

    return true
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
    local success, err = self.state:set_agent_config(agent_name, self.state.model)
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

-- Change model (implementation was missing in original)
function controller:change_model(model_name)
    if not model_name then
        return nil, ERR.MODEL_NAME_REQUIRED
    end

    -- Reset agent instance to force reload with new model
    self.agent = nil

    -- Update model in state
    local success, err = self.state:set_agent_config(self.state.agent_name, model_name)
    if not success then
        return nil, err
    end

    -- Notify clients about model change
    if self.upstream then
        self.upstream:update_session({
            model = model_name
        })
    end

    return true
end

-- Handle stop command
function controller:handle_stop_command()
    -- Mark stop requested - will be checked during processing
    self.stop_requested = true

    -- Clear any pending next payload
    self.next_payload = nil

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
    elseif command == controller.CMD.TOOLS then
        if payload.tool_ids then
            return self:set_tool_ids(payload.tool_ids)
        end
        return nil, "Tool IDs required"
    else
        return nil, "Unsupported command: " .. command
    end
end

-- Cancel processing
function controller:cancel()
    -- Mark stop requested
    self.stop_requested = true

    -- If currently processing with an agent, try to cancel its operation
    if self.agent and self.agent.cancel then
        self.agent:cancel()
    end

    -- Reset processing state
    self.is_processing = false

    -- Clear any pending next payload
    self.next_payload = nil

    return true
end

-- Initialize controller with agent and model
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

    return true
end

return controller

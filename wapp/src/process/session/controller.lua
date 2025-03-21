local uuid = require("uuid")
local prompt = require("prompt")
local json = require("json")
local agent_manager = require("agent_manager")
local tool_manager = require("tool_manager")

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

-- Controller class
local controller = {}
controller.__index = controller

-- Constructor
function controller.new(session_state, upstream)
    local self = setmetatable({}, controller)

    -- Store dependencies
    self.state = session_state
    self.upstream = upstream

    -- Create component managers
    self.tool_manager = tool_manager.new(session_state, upstream)
    self.agent_manager = agent_manager.new(session_state, upstream)

    -- Track processing state
    self.is_processing = false
    self.stop_requested = false

    -- Next payload for continuation (if any)
    self.next_payload = nil

    return self
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
                -- For function messages that contain both call and result
                if meta.function_name and meta.status then
                    if meta.status == tool_manager.FUNC_STATUS.PENDING then
                        -- This is a function call
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
                    elseif meta.status == tool_manager.FUNC_STATUS.SUCCESS or
                        meta.status == tool_manager.FUNC_STATUS.ERROR then
                        -- This is a function result
                        builder:add_function_result(
                            meta.function_name,
                            msg.data,
                            meta.function_call_id
                        )
                    end
                end
            end
        end
    end

    return builder
end

-- Process a normal agent response
function controller:process_normal_response(result, message_id, response_id)
    -- Check if we have tool calls to process
    if result.tool_calls and #result.tool_calls > 0 then
        -- Process tool calls
        local success, tool_result = self.tool_manager:process_tool_calls(
            result.tool_calls,
            message_id,
            response_id
        )

        if not success then
            return false, "Tool call failed: " .. (tool_result or "Unknown error")
        end

        -- Handle special control results (like model change or delegation)
        if type(tool_result) == "table" and tool_result.type then
            if tool_result.type == "model_change" then
                -- Change the model
                local change_success, change_err = self.agent_manager:change_model(
                    tool_result.target_model,
                    tool_result.reason
                )

                if not change_success then
                    return false, "Model change failed: " .. change_err
                end
            elseif tool_result.type == "delegate" then
                -- Create delegation data
                local delegation_data = {
                    target_agent = tool_result.target_agent,
                    reason = tool_result.reason,
                    message = tool_result.message,
                    result = result.result
                }

                -- Process delegation
                local delegate_success, continuation = self.agent_manager:process_delegation(
                    delegation_data,
                    message_id,
                    response_id
                )

                if delegate_success then
                    self.next_payload = continuation
                    return true
                else
                    return false, "Delegation failed: " .. (continuation or "Unknown error")
                end
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

            -- Send the partial content update (no detailed tool info)
            self.upstream:send_message_update(response_id, "content", {
                content = result.result,
                using_tools = true
            })
        end

        -- Get the current agent
        local agent, err = self.agent_manager:get_agent()
        if err then
            return false, "Failed to get agent: " .. err
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
        local next_result, err = agent:step(prompt_builder, stream_options)

        -- Check for errors
        if err then
            self.upstream:message_error(message_id, "AGENT_ERROR", err)
            return false, err
        end

        -- Recursively process the next result
        if next_result.delegate_target then
            -- Agent wants to delegate
            local delegation_data = {
                target_agent = next_result.delegate_target,
                reason = next_result.delegate_reason,
                message = next_result.delegate_message,
                result = next_result.result
            }

            -- Process delegation
            local delegate_success, continuation = self.agent_manager:process_delegation(
                delegation_data,
                message_id,
                response_id
            )

            if delegate_success then
                self.next_payload = continuation
                return true
            else
                return false, "Delegation failed: " .. (continuation or "Unknown error")
            end
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
            local stored_response_id, err = self.state:add_assistant_message(
                next_result.result,
                metadata
            )

            if err then
                return false, "Failed to store response: " .. err
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

-- Handle user message
function controller:handle_message(message_data)
    -- Validate
    if not message_data.text or message_data.text == "" then
        return nil, agent_manager.ERR.EMPTY_MESSAGE
    end

    -- Check session status from state
    if self.state.status == STATUS.FAILED then
        return nil, agent_manager.ERR.FAILED_STATUS
    end

    -- Check if already processing
    if self.is_processing then
        return nil, agent_manager.ERR.BUSY
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
        return nil, "Failed to generate response ID"
    end

    -- Announce the response is beginning
    self.upstream:response_beginning(message_id, response_id)

    -- Get the agent
    local agent, err = self.agent_manager:get_agent()
    if err then
        self.is_processing = false
        self.upstream:message_error(message_id, "AGENT_ERROR", err)
        return nil, err
    end

    -- Build prompt
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
        local delegation_data = {
            target_agent = result.delegate_target,
            reason = result.delegate_reason,
            message = result.delegate_message,
            result = result.result
        }

        -- Process delegation
        local success, continuation = self.agent_manager:process_delegation(
            delegation_data,
            message_id,
            response_id
        )

        if not success then
            self.is_processing = false
            self.upstream:message_error(message_id, "DELEGATION_ERROR", continuation or "Delegation failed")
            return nil, continuation or "Delegation failed"
        end

        -- Store continuation data
        self.next_payload = continuation

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

-- Handle continue command
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
    local agent, err = self.agent_manager:get_agent()
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

    -- Process the result
    local final_result = {}

    if result.delegate_target then
        -- Agent wants to delegate again
        local delegation_data = {
            target_agent = result.delegate_target,
            reason = result.delegate_reason,
            message = result.delegate_message,
            result = result.result
        }

        -- Process delegation
        local success, continuation = self.agent_manager:process_delegation(
            delegation_data,
            message_id,
            response_id
        )

        if not success then
            return false, continuation or "Delegation failed"
        end

        -- Store continuation data
        self.next_payload = continuation

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

-- Generate a title using the agent
function controller:generate_title(prompt_builder)
    -- Get an agent instance
    local agent, err = self.agent_manager:get_agent()
    if err then
        return nil, err
    end

    -- Generate a title using the agent's model
    local result, err = agent:generate_title(prompt_builder)
    if err then
        return nil, err
    end

    return result
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
        return self.agent_manager:change_model(
            payload.name,
            payload.reason or "Changed by user"
        )
    elseif command == COMMANDS.AGENT then
        if not payload.name then
            return nil, "Agent name required"
        end
        return self.agent_manager:change_agent(
            payload.name,
            payload.reason or "Changed by user"
        )
    elseif command == COMMANDS.TOOLS then
        if not payload.tool_ids then
            return nil, "Tool IDs required"
        end
        return self.tool_manager:set_tool_ids(payload.tool_ids)
    else
        return nil, "Unsupported command: " .. command
    end
end

-- Cancel processing
function controller:cancel()
    -- Mark stop requested
    self.stop_requested = true

    -- If currently processing with an agent, try to cancel its operation
    local agent = self.agent_manager:get_agent()
    if agent and agent.cancel then
        agent:cancel()
    end

    -- Reset processing state
    self.is_processing = false

    -- Clear any pending next payload
    self.next_payload = nil

    return true
end

-- Initialize controller with agent and model
function controller:init(agent_name, model)
    -- Change to the specified agent
    local success, err = self.agent_manager:change_agent(
        agent_name,
        "Initial configuration"
    )

    if not success then
        return nil, err
    end

    -- If model is specified, change to it
    if model then
        success, err = self.agent_manager:change_model(
            model,
            "Initial configuration"
        )

        if not success then
            return nil, err
        end
    end

    return true
end

-- Export constants
controller.CMD = COMMANDS
controller.STATUS = STATUS

return controller

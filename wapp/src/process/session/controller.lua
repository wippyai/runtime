local uuid = require("uuid")
local json = require("json")
local agent_registry = require("agent_registry")
local agent_runner = require("agent_runner")
local tool_caller = require("tool_caller")
local prompt_builder = require("prompt_builder")
local queue = require("queue")
local consts = require("consts")

-- Use constants from consts package
local STATUS = consts.STATUS
local TASK_TYPE = consts.TASK_TYPE
local COMMANDS = consts.COMMANDS
local MSG_TYPE = consts.MSG_TYPE
local FUNC_STATUS = consts.FUNC_STATUS
local CONTROL_TYPE = consts.CONTROL_TYPE
local ERR = consts.ERR

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
    self.tool_caller = tool_caller.new()
    self.prompt_builder = prompt_builder.new(session_state)

    -- Own the agent instance
    self.agent = nil -- Will be lazy-loaded

    self.stop_requested = false

    -- Create task queue
    self.task_queue = queue.new()

    return self
end

-- Check if there are more tasks to process
function controller:has_next()
    return not self.task_queue:is_empty()
end

-- Initialize controller with agent and model
function controller:init(agent_name, model)
    -- Schedule agent change
    self.task_queue:enqueue({
        type = TASK_TYPE.AGENT_CHANGE,
        agent_name = agent_name,
        init = true
    })

    -- If model is specified, schedule model change after agent
    if model then
        self.task_queue:enqueue({
            type = TASK_TYPE.MODEL_CHANGE,
            model_name = model,
            init = true
        })
    end

    return true
end

-- Cancel processing
function controller:cancel()
    self.stop_requested = true
    self.task_queue:clear()
    return true
end

-- Process next task from the queue
function controller:process_next()
    if self.task_queue:is_empty() then
        return nil, ERR.QUEUE_EMPTY
    end

    local task = self.task_queue:dequeue()

    local result, err = self:process_task(task)
    if err then
        self.task_queue:clear()
    end

    return result, err
end

-- Main task processing function
function controller:process_task(task)
    self.stop_requested = false

    local message_id

    -- Process different task types
    if task.type == TASK_TYPE.MESSAGE then
        local message_text = task.text

        message_id = self.state:add_user_message(message_text, { file_uuids = task.file_uuids })
        if not message_id then
            return nil, "Failed to store message"
        end

        -- Notify clients about message reception
        self.upstream:message_received(message_id, message_text)
    elseif task.type == TASK_TYPE.AGENT_CHANGE then
        -- Change the agent
        if not task.agent_name then
            return nil, ERR.AGENT_NAME_REQUIRED
        end

        local success, err = self:change_agent(task.agent_name, task.init)
        if not success then
            return nil, err
        end

        return { task_completed = true, type = TASK_TYPE.AGENT_CHANGE }
    elseif task.type == TASK_TYPE.MODEL_CHANGE then
        -- Change the model
        if not task.model_name then
            return nil, ERR.MODEL_NAME_REQUIRED
        end

        local success, err = self:change_model(task.model_name, task.init)
        if not success then
            return nil, err
        end

        return { task_completed = true, type = TASK_TYPE.MODEL_CHANGE }
    elseif task.type == TASK_TYPE.TOOL_CONTINUE or task.type == TASK_TYPE.DELEGATION then
        if task.message_id then
            message_id = task.message_id -- traced
        else
            print("MISS")
        end
    else
        return false, "Invalid task type: " .. (task.type or "nil")
    end

    if not message_id then
        print(json.encode(task))
        return false, "Message ID not set"
    end

    -- Get the agent
    local agent, err = self:get_agent()
    if err then
        return false, err
    end

    -- We can control what we pass to the agent here
    local builder, prompt_err = self.prompt_builder:build_prompt()
    if not builder then
        return false, prompt_err or "Failed to build prompt"
    end

    -- Expected agent response id
    local response_id, err = uuid.v7()
    if err then
        return nil, ERR.RESPONSE_ID_FAILED
    end

    self.upstream:response_beginning(response_id, message_id)

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
        self.upstream:message_error(response_id, "AGENT_ERROR", exec_err)
        return false, exec_err
    end

    if result.result and result.result ~= "" then
        local _, err = self.state:add_assistant_message(result.result, {
            source_id = message_id,
            message_id = response_id,
            agent_name = self.state.agent_name,
            model = self.state.model,
            tokens = result.tokens,
            has_tool_calls = (result.tool_calls and #result.tool_calls > 0)
        })

        if err then
            self.upstream:message_error(response_id, "STORAGE_ERROR", err)
            return false, "Failed to store response: " .. err
        end

        self.upstream:send_message_update(response_id, "content", {
            content = result.result,
            using_tools = (result.tool_calls and #result.tool_calls > 0)
        })
    else
        self.upstream:invalidate_message(response_id)
    end

    -- Check if stop was requested during processing
    if self.stop_requested then
        return {
            message_id = message_id,
            response_id = response_id,
            stopped = true
        }
    end

    -- Process agent result
    local final_result = {
        message_id = message_id,
        result = result.result,
        tokens = result.tokens
    }

    result.message_id = message_id

    if result.delegate_target then
        local _, err = self:handle_delegation(result, true) -- let next agent continue
        if err then
            return false, "Failed to handle delegation: " .. err
        end
    end

    if result.tool_calls and #result.tool_calls > 0 then
        local _, err = self:handle_tool_calls(result)
        if err then
            return false, "Failed to handle tool calls: " .. err
        end
    end

    return final_result
end

-- Handle external user message
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

    -- Enqueue message processing task
    self.task_queue:enqueue({
        type = TASK_TYPE.MESSAGE,
        text = message_data.text,
        file_uuids = message_data.file_uuids
    })

    return {
        scheduled = true
    }
end

-- Handle external commands
function controller:handle_command(command, payload)
    if command == COMMANDS.STOP then
        self.stop_requested = true
        self.task_queue:clear()
        return true
    elseif command == COMMANDS.AGENT then
        if not payload or not payload.name then
            return false, ERR.AGENT_NAME_REQUIRED
        end

        self.task_queue:enqueue({
            type = TASK_TYPE.AGENT_CHANGE,
            agent_name = payload.name
        })

        return true
    elseif command == COMMANDS.MODEL then
        if not payload or not payload.name then
            return false, ERR.MODEL_NAME_REQUIRED
        end

        self.task_queue:enqueue({
            type = TASK_TYPE.MODEL_CHANGE,
            model_name = payload.name
        })

        return true
    else
        return false, ERR.UNSUPPORTED_COMMAND
    end
end

-- Handle delegation (direct delegation, not via tool)
function controller:handle_delegation(result, enqueue)
    if not result.delegate_target then
        return false, "No delegation target specified"
    end

    -- todo: add proper loop detection

    -- Add a delegation record in state
    local delegation_id = self.state:add_message(MSG_TYPE.DELEGATION, "", {
        from_agent = self.state.agent_name,
        to_agent = result.delegate_target,
        message = result.delegate_message or "Continuing with specialized agent"
    })

    -- Switch agent
    local success, change_err = self:change_agent(result.delegate_target)
    if not success then
        return false, "Failed to switch agent: " .. change_err
    end

    -- Continue in a loop
    if enqueue then
        self.task_queue:enqueue({
            type = TASK_TYPE.DELEGATION,
            message_id = result.message_id
        })
    end

    return true
end

-- Handle tool calls
function controller:handle_tool_calls(result)
    -- Phase 1: Validate and pre-process tool calls
    local validated_tools, validate_err = self.tool_caller:validate(result.tool_calls)
    if validate_err then
        if not validated_tools then
            return false, "Tool validation failed: " .. validate_err
        else
            print("skipping some tool calls")
        end
    end

    -- Log tool calls to state and notify clients
    for call_id, tool_call in pairs(validated_tools) do
        -- We can do custom pre-filter or handling here
        if tool_call.valid then
            -- At this point we can add custom tool handler since we know both tool call and meta

            self.state:add_function_call(tool_call.name, tool_call.args, {
                call_id = call_id,
                message_id = tool_call.call_id,
                registry_id = tool_call.registry_id,
            })
            self.upstream:send_message_update(call_id, "function_call", { function_name = tool_call.name })
        end
    end

    -- Phase 2: Execute the validated tools
    local results = self.tool_caller:execute(validated_tools) -- todo: can be made cancellable

    -- Process results and handle control results
    for _, result_data in pairs(results) do
        call_id = result_data.call.call_id
        if result_data.error then
            self.state:update_function_result(call_id, tostring(result_data.error), false)
            self.upstream:send_message_update(call_id, "function_error", {
                call_id = call_id,
                function_name = result_data.call.name,
                error = "Function execution failed"
            })

            goto continue
        end

        local meta = result_data.call.meta
        local tool_result = result_data.result

        -- alway store as json and always unpack as json
        self.state:update_function_result(call_id, tool_result, true)
        self.upstream:send_message_update(call_id, "function_success", {
            call_id = call_id,
            function_name = result_data.call.name
        })

        -- Post hooks!

        -- Check for model controller tool
        if meta.model_controller and type(tool_result) == "table" and tool_result.target_model then
            -- Handle model change
            local change_success, change_err = self:change_model(tool_result.target_model)
            if not change_success then
                goto continue
            end
        end

        -- Check for delegate controller tool
        if meta.delegate_controller and type(tool_result) == "table" and tool_result.target_agent then
            local success, err = self:handle_delegation({
                delegate_target = tool_result.target_agent,
                delegate_message = tool_result.message
            }, false)

            if not success then
                goto continue
            end
        end

        ::continue::
    end

    -- Enqueue continuation task for after tool call processing
    self.task_queue:enqueue({ type = TASK_TYPE.TOOL_CONTINUE, message_id = result.message_id })

    return true
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

-- Change to a different agent
function controller:change_agent(agent_name, init)
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
        self:change_model(agent_spec.model)
    end

    -- Record the agent change if previous agent was set
    if previous_agent then
        if self.upstream and not init then
            self.upstream:update_session({
                agent = agent_name,
                model = self.state.model,
            })
        end

        -- Create metadata for agent change
        local metadata = {
            system_action = "agent_change",
            from_agent = previous_agent,
            to_agent = agent_name
        }

        -- Add agent_change record without message text
        self.state:add_message(MSG_TYPE.AGENT_CHANGE, "", metadata)
    end

    return true
end

-- Change model
function controller:change_model(model_name, init)
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

    -- Record the model change if previous model was set
    if previous_model and previous_model ~= "" then
        if self.upstream and not init then
            self.upstream:update_session({
                model = model_name
            })
        end

        -- Create metadata for model change
        local metadata = {
            system_action = "model_change",
            from_model = previous_model,
            to_model = model_name
        }

        -- Add model_change record without message text
        self.state:add_message(MSG_TYPE.MODEL_CHANGE, "", metadata)
    end

    return true
end

-- Export constants for external use
controller.STATUS = STATUS
controller.TASK_TYPE = TASK_TYPE

return controller

local json = require("json")
local uuid = require("uuid")
local tool_caller = require("tool_caller")

-- Function status constants (duplicated here for clarity)
local FUNC_STATUS = {
    PENDING = "pending",
    SUCCESS = "success",
    ERROR = "error"
}

-- ToolManager class
local tool_manager = {}
tool_manager.__index = tool_manager

-- Constructor
function tool_manager.new(session_state, upstream)
    local self = setmetatable({}, tool_manager)

    -- Store dependencies
    self.state = session_state
    self.upstream = upstream
    self.tool_caller = tool_caller.new()

    return self
end

-- Set tool IDs for the session
function tool_manager:set_tool_ids(tool_ids)
    -- Store tool IDs in state
    self.state.tool_ids = tool_ids

    -- Notify clients about tool configuration
    if self.upstream then
        self.upstream:update_session({
            tools = tool_ids
        })
    end

    return true
end

-- Process a list of tool calls
function tool_manager:process_tool_calls(tool_calls, message_id, response_id)
    -- Check if there are any tool calls
    if not tool_calls or #tool_calls == 0 then
        return true, nil
    end

    -- Check for special control tools first
    local control_tool = self.tool_caller:check_control_tools(tool_calls)
    if control_tool then
        -- This is a control tool (model_controller or delegate_controller)
        return self:process_control_tool(control_tool, message_id, response_id)
    end

    -- Validate concurrent execution
    local can_execute, conflicts = self.tool_caller:validate_concurrent_tools(tool_calls)
    if not can_execute then
        -- There are conflicts - cannot execute these tools together
        for _, conflict in ipairs(conflicts) do
            -- Store error result for conflicting tool
            self:log_tool_execution(
                conflict.id,
                conflict.name,
                { error = conflict.reason },
                message_id,
                response_id,
                true
            )
        end
        return false, "Tool execution conflict: " .. conflicts[1].reason
    end

    -- Process each tool call sequentially
    for i, tool_call in ipairs(tool_calls) do
        local tool_name = tool_call.name
        local arguments = tool_call.arguments
        local registry_id = tool_call.registry_id
        local function_call_id = tool_call.id or uuid.v7()

        -- Stream only tool name to client (no IDs, no args)
        self.upstream:send_message_update(response_id, "tool_call", {
            function_name = tool_name
        })

        -- Store function call in session state - use new combined message format
        local metadata = {
            function_name = tool_name,
            function_call_id = function_call_id,
            registry_id = registry_id,
            status = FUNC_STATUS.PENDING,
            response_id = response_id,
            message_id = message_id
        }

        self.state:add_message(
            "function",
            arguments,
            metadata
        )

        -- Execute the tool function
        local result, err = self.tool_caller:call_tool(registry_id, arguments)

        -- Update the function message with result or error
        if err then
            -- Update metadata with error status
            metadata.status = FUNC_STATUS.ERROR
            metadata.error = err

            -- Stream minimal error to client (only tool name)
            self.upstream:send_message_update(response_id, "tool_error", {
                function_name = tool_name,
                error = "Tool execution failed"
            })

            -- Update the function message
            self.state:update_message_metadata(function_call_id, metadata)
            self.state:update_message_data(function_call_id, json.encode({ error = err }))
        else
            -- Update metadata with success status
            metadata.status = FUNC_STATUS.SUCCESS

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

            -- Update the function message
            self.state:update_message_metadata(function_call_id, metadata)
            self.state:update_message_data(function_call_id, result_content)
        end
    end

    return true, nil
end

-- Process a control tool (model_controller or delegate_controller)
function tool_manager:process_control_tool(control_tool, message_id, response_id)
    local tool_call = control_tool.call
    local meta = control_tool.meta

    -- Generate function call ID
    local function_call_id = tool_call.id or uuid.v7()

    -- Store function call in session state (same format as normal tools)
    local metadata = {
        function_name = tool_call.name,
        function_call_id = function_call_id,
        registry_id = tool_call.registry_id,
        status = FUNC_STATUS.PENDING,
        response_id = response_id,
        message_id = message_id
    }

    if control_tool.type == tool_caller.TOOL_TYPES.DELEGATE_CONTROLLER then
        metadata.control_type = "delegate"
    else
        metadata.control_type = "model_change"
    end

    -- Create the function message
    self.state:add_message(
        "function",
        tool_call.arguments,
        metadata
    )

    -- Execute the tool
    local result, err = self.tool_caller:call_tool(tool_call.registry_id, tool_call.arguments)

    if err then
        -- Update metadata with error status
        metadata.status = FUNC_STATUS.ERROR
        metadata.error = err

        -- Update the function message
        self.state:update_message_metadata(function_call_id, metadata)
        self.state:update_message_data(function_call_id, json.encode({ error = err }))

        return false, "Control tool execution failed: " .. err
    end

    -- Update metadata with success status
    metadata.status = FUNC_STATUS.SUCCESS

    -- Process result based on control type
    if control_tool.type == tool_caller.TOOL_TYPES.DELEGATE_CONTROLLER then
        -- Delegate to another agent
        if type(result) == "string" then
            -- Try to parse JSON
            local success, parsed = pcall(json.decode, result)
            if success then
                result = parsed
            else
                result = { error = "Invalid delegate result format" }
                metadata.status = FUNC_STATUS.ERROR
                metadata.error = "Invalid result format"
            end
        end

        if type(result) ~= "table" or not result.target_agent then
            result = { error = "Missing target_agent in delegate result" }
            metadata.status = FUNC_STATUS.ERROR
            metadata.error = "Missing target_agent"
        else
            -- Store the successful result
            metadata.target_agent = result.target_agent
            metadata.reason = result.reason or "Delegation requested"
            metadata.message = result.message

            -- Store result in session state
            local result_content = json.encode(result)

            -- Update the function message
            self.state:update_message_metadata(function_call_id, metadata)
            self.state:update_message_data(function_call_id, result_content)

            -- Return delegation information for the controller to handle
            return true, {
                type = "delegate",
                target_agent = result.target_agent,
                reason = result.reason,
                message = result.message
            }
        end
    else
        -- Model change
        if type(result) == "string" then
            -- Try to parse JSON
            local success, parsed = pcall(json.decode, result)
            if success then
                result = parsed
            else
                result = { error = "Invalid model change result format" }
                metadata.status = FUNC_STATUS.ERROR
                metadata.error = "Invalid result format"
            end
        end

        if type(result) ~= "table" or not result.target_model then
            result = { error = "Missing target_model in model change result" }
            metadata.status = FUNC_STATUS.ERROR
            metadata.error = "Missing target_model"
        else
            -- Store the successful result
            metadata.target_model = result.target_model
            metadata.reason = result.reason or "Model change requested"

            -- Update the function message
            self.state:update_message_metadata(function_call_id, metadata)
            self.state:update_message_data(function_call_id, json.encode(result))

            -- Return model change information for the controller to handle
            return true, {
                type = "model_change",
                target_model = result.target_model,
                reason = result.reason or "Model change requested via tool"
            }
        end
    end

    -- Store final result in session state if we got here (probably an error)
    local result_content = json.encode(result)

    -- Update the function message
    self.state:update_message_metadata(function_call_id, metadata)
    self.state:update_message_data(function_call_id, result_content)

    if metadata.status == FUNC_STATUS.ERROR then
        return false, metadata.error
    end

    return true, nil
end

-- Log a tool execution (for both normal and error cases)
function tool_manager:log_tool_execution(tool_id, tool_name, result, message_id, response_id, is_error)
    local function_call_id = uuid.v7()

    -- Prepare metadata
    local metadata = {
        function_name = tool_name,
        function_call_id = function_call_id,
        registry_id = tool_id,
        status = is_error and FUNC_STATUS.ERROR or FUNC_STATUS.SUCCESS,
        response_id = response_id,
        message_id = message_id
    }

    if is_error then
        metadata.error = result.error
    end

    -- Convert result to string
    local result_content = result
    if type(result) == "table" then
        result_content = json.encode(result)
    elseif type(result) ~= "string" then
        result_content = tostring(result)
    end

    -- Store in session state
    local stored_id = self.state:add_message(
        "function",
        result_content,
        metadata
    )

    -- todo: updaye meta!

    return stored_id
end

-- Export constants
tool_manager.FUNC_STATUS = FUNC_STATUS

return tool_manager
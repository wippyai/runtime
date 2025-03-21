local json = require("json")
local uuid = require("uuid")
local tools = require("tools")
local funcs = require("funcs")

-- Function status constants
local FUNC_STATUS = {
    PENDING = "pending",
    SUCCESS = "success",
    ERROR = "error"
}

-- Tool Caller class
local tool_caller = {}
tool_caller.__index = tool_caller

-- Constructor
function tool_caller.new(session_state, upstream)
    local self = setmetatable({}, tool_caller)

    -- Store dependencies
    self.state = session_state
    self.upstream = upstream
    self.executor = funcs.new()

    return self
end

-- Process a list of tool calls
function tool_caller:process_tool_calls(tool_calls, message_id, response_id)
    -- Check if there are any tool calls
    if not tool_calls or #tool_calls == 0 then
        return true, nil
    end

    -- Process each tool call sequentially
    for _, tool_call in ipairs(tool_calls) do
        local tool_name = tool_call.name
        local arguments = tool_call.arguments
        local registry_id = tool_call.registry_id
        local function_call_id = tool_call.id or uuid.v7()

        -- Notify client about tool call
        self.upstream:send_message_update(response_id, "tool_call", {
            function_name = tool_name
        })

        -- Store function call in session state
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

        -- Execute the tool
        local result, err = self.executor:call(registry_id, arguments)

        -- Handle error case
        if err then
            -- Update metadata with error status
            metadata.status = FUNC_STATUS.ERROR
            metadata.error = err

            -- Notify client about error
            self.upstream:send_message_update(response_id, "tool_error", {
                function_name = tool_name,
                error = "Tool execution failed"
            })

            -- Update the function message
            self.state:update_message_metadata(function_call_id, metadata)
            self.state:update_message_data(function_call_id, json.encode({ error = err }))

            goto continue
        end

        -- Handle success case
        -- Get tool schema to check if this is a special control tool
        local schema, _ = tools.get_tool_schema(registry_id)
        local control_result = nil

        -- Set success status
        metadata.status = FUNC_STATUS.SUCCESS

        -- Parse string results if needed
        if type(result) == "string" then
            local parsed, parse_err = json.decode(result)
            if not parse_err then
                result = parsed
            end
        end

        -- For model_controller tools
        if schema and schema.meta and schema.meta.model_controller and type(result) == "table" then
            if result.target_model then
                metadata.control_type = "model_change"
                metadata.target_model = result.target_model
                metadata.reason = result.reason

                control_result = {
                    type = "model_change",
                    target_model = result.target_model,
                    reason = result.reason or "Model change requested"
                }
            else
                metadata.error = "Invalid model change result: missing target_model"
            end
        end

        -- Convert result to string for storage
        local result_content = result
        if type(result) == "table" then
            result_content = json.encode(result)
        elseif type(result) ~= "string" then
            result_content = tostring(result)
        end

        -- Notify client about success
        self.upstream:send_message_update(response_id, "tool_result", {
            function_name = tool_name,
            status = "success"
        })

        -- Update the function message
        self.state:update_message_metadata(function_call_id, metadata)
        self.state:update_message_data(function_call_id, result_content)

        -- Return control result if any
        if control_result then
            return true, control_result
        end

        ::continue::
    end

    return true, nil
end

-- Export constants
tool_caller.FUNC_STATUS = FUNC_STATUS

return tool_caller
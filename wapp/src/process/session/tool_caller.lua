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
function tool_caller.new()
    local self = setmetatable({}, tool_caller)
    self.executor = funcs.new()
    return self
end

-- Validate tool calls and return enriched data
function tool_caller:validate(tool_calls)
    -- Check if there are any tool calls
    if not tool_calls or #tool_calls == 0 then
        return {}, nil
    end

    local validated_tools = {}
    local has_exclusive = false
    local exclusive_tool = nil

    -- First pass: check for exclusive tools and get metadata
    for _, tool_call in ipairs(tool_calls) do
        local tool_name = tool_call.name
        local arguments = tool_call.arguments
        local registry_id = tool_call.registry_id
        local function_call_id = uuid.v7()

        -- Get tool schema with metadata
        local schema, err = tools.get_tool_schema(registry_id)
        if err then
            -- Add to validated tools with error
            validated_tools[function_call_id] = {
                call_id = function_call_id,
                name = tool_name,
                args = arguments,
                registry_id = registry_id,
                meta = {},
                error = "Failed to get tool schema: " .. err,
                valid = false
            }
            goto continue
        end

        local meta = schema and schema.meta or {}

        -- Check if this is an exclusive tool
        if meta.exclusive then
            if has_exclusive then
                -- Multiple exclusive tools found - error
                return {}, "Multiple exclusive tools found, cannot process"
            end

            has_exclusive = true
            exclusive_tool = function_call_id
        end

        -- Add to validated tools
        validated_tools[function_call_id] = {
            call_id = function_call_id,
            name = tool_name,
            args = arguments,
            registry_id = registry_id,
            meta = meta,
            valid = true
        }

        ::continue::
    end

    -- Second pass: if we have an exclusive tool, only keep that one
    if has_exclusive and #tool_calls > 1 then
        local exclusive_data = validated_tools[exclusive_tool]
        local result = {}
        result[exclusive_tool] = exclusive_data

        -- Return only the exclusive tool and a message about the others being skipped
        return result, "Exclusive tool found, other tools skipped"
    end

    return validated_tools, nil
end

-- Execute validated tools
function tool_caller:execute(validated_tools)
    local results = {}

    for call_id, tool_call in pairs(validated_tools) do
        if not tool_call.valid then
            -- doing nothing
            goto continue
        end

        local registry_id = tool_call.registry_id
        local args = tool_call.args

        if type(args) == "string" then
            local parsed_args, err = json.decode(args)
            if err then
                results[call_id] = {
                    error = "Failed to parse arguments: " .. err,
                    call = tool_call
                }
                goto continue
            end
            args = parsed_args
        end

        -- Execute the tool
        local result, err = self.executor:call(registry_id, args)
        if err then
            -- Record error
            results[call_id] = {
                error = err,
                call = tool_call
            }
            goto continue
        end

        results[call_id] = {
            result = result,
            call = tool_call
        }

        ::continue::
    end

    return results
end

-- Export constants
tool_caller.FUNC_STATUS = FUNC_STATUS

return tool_caller

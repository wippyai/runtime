local json = require("json")
local funcs = require("funcs")
local tools = require("tools")

-- Tool Caller module constants
local TOOL_TYPES = {
    MODEL_CONTROLLER = "model_controller",
    DELEGATE_CONTROLLER = "delegate_controller",
    NORMAL = "normal"
}

-- ToolCaller class
local tool_caller = {}
tool_caller.__index = tool_caller

-- Constructor
function tool_caller.new(executor, tool_resolver)
    local self = setmetatable({}, tool_caller)

    -- Store dependencies (inject or default)
    self.executor = executor or funcs.new()
    self.tool_resolver = tool_resolver or tools

    -- Cache for tool metadata
    self.meta_cache = {}

    return self
end

-- Get metadata for a list of tool IDs
function tool_caller:get_tools_meta(tool_ids)
    if not tool_ids or #tool_ids == 0 then
        return {}
    end

    local schemas, errors = self.tool_resolver.get_tool_schemas(tool_ids)
    if not schemas then
        return {}
    end

    local result = {}
    for id, schema in pairs(schemas) do
        table.insert(result, self:extract_control_properties(id, schema))
    end

    return result
end

-- Extract control properties from tool schema
function tool_caller:extract_control_properties(id, schema)
    -- If we have it in cache, use cached version
    if self.meta_cache[id] then
        return self.meta_cache[id]
    end

    -- Tool schema should already have meta from tool_resolver
    local meta = schema.meta or {}

    -- Extract control properties
    local tool_meta = {
        id = id,
        name = schema.name,
        title = schema.title,
        description = schema.description,
        schema = schema.schema,
        exclusive = meta.exclusive or false,
        delegate_controller = meta.delegate_controller or false,
        model_controller = meta.model_controller or false,
        output_schema = meta.output_schema
    }

    -- Cache the metadata
    self.meta_cache[id] = tool_meta

    return tool_meta
end

-- Get metadata for a single tool ID
function tool_caller:get_tool_meta(tool_id)
    if not tool_id then
        return nil, "Tool ID is required"
    end

    -- Check cache first
    if self.meta_cache[tool_id] then
        return self.meta_cache[tool_id]
    end

    -- Use tool resolver to get schema
    local schema, err = self.tool_resolver.get_tool_schema(tool_id)
    if not schema then
        return nil, "Failed to get tool schema: " .. (err or "Unknown error")
    end

    -- Extract control properties and cache
    return self:extract_control_properties(tool_id, schema)
end

-- Execute a tool with given arguments
function tool_caller:call_tool(tool_id, arguments)
    if not tool_id then
        return nil, "Tool ID is required"
    end

    -- Get tool metadata (validates the tool exists)
    local meta, err = self:get_tool_meta(tool_id)
    if not meta then
        return nil, "Failed to get tool metadata: " .. (err or "Unknown error")
    end

    -- Execute the tool
    local result, err = self.executor:call(tool_id, arguments)

    if err then
        return nil, err
    end

    -- For controller tools, add control_type to the result
    if meta.delegate_controller or meta.model_controller then
        -- Make sure result is a table
        if type(result) ~= "table" then
            -- Try to parse as JSON if it's a string
            if type(result) == "string" then
                local success, parsed = pcall(json.decode, result)
                if success then
                    result = parsed
                else
                    result = { raw_output = result }
                end
            else
                result = { raw_output = tostring(result) }
            end
        end

        -- Add control_type to the result
        result._control_type = meta.delegate_controller and TOOL_TYPES.DELEGATE_CONTROLLER or TOOL_TYPES.MODEL_CONTROLLER
    end

    return result
end

-- Check if any tools in the list are special control tools
function tool_caller:check_control_tools(tool_calls)
    if not tool_calls or #tool_calls == 0 then
        return nil
    end

    for _, call in ipairs(tool_calls) do
        if not call.registry_id then
            goto continue
        end

        -- Get metadata
        local meta, err = self:get_tool_meta(call.registry_id)
        if not meta then
            goto continue
        end

        -- Check if it's a control tool
        if meta.model_controller then
            return {
                type = TOOL_TYPES.MODEL_CONTROLLER,
                call = call,
                meta = meta
            }
        elseif meta.delegate_controller then
            return {
                type = TOOL_TYPES.DELEGATE_CONTROLLER,
                call = call,
                meta = meta
            }
        end

        ::continue::
    end

    return nil
end

-- Resolve tool name to ID within a scope
function tool_caller:resolve_tool_name(name, scope_ids)
    return self.tool_resolver.resolve_name_to_id(name, scope_ids)
end

-- Check if tools can be executed concurrently
function tool_caller:validate_concurrent_tools(tool_calls)
    if not tool_calls or #tool_calls <= 1 then
        return true, {}  -- No concurrency issues with 0 or 1 tools
    end

    local exclusive_tools = {}
    local exclusive_conflicts = {}

    -- First pass: identify exclusive tools
    for i, call in ipairs(tool_calls) do
        if not call.registry_id then
            goto continue
        end

        -- Get metadata
        local meta, err = self:get_tool_meta(call.registry_id)
        if not meta then
            goto continue
        end

        -- Check if exclusive
        if meta.exclusive then
            table.insert(exclusive_tools, {
                index = i,
                id = call.registry_id,
                name = meta.name
            })
        end

        ::continue::
    end

    -- If no exclusive tools, then all can run concurrently
    if #exclusive_tools == 0 then
        return true, {}
    end

    -- If only one exclusive tool, check if it's the only tool
    if #exclusive_tools == 1 and #tool_calls > 1 then
        return false, {
            {
                index = exclusive_tools[1].index,
                id = exclusive_tools[1].id,
                name = exclusive_tools[1].name,
                reason = "This tool cannot be called with other tools"
            }
        }
    end

    -- If multiple exclusive tools, none can run
    if #exclusive_tools > 1 then
        for _, tool in ipairs(exclusive_tools) do
            table.insert(exclusive_conflicts, {
                index = tool.index,
                id = tool.id,
                name = tool.name,
                reason = "This tool cannot be called with other exclusive tools"
            })
        end
        return false, exclusive_conflicts
    end

    return true, {}
end

-- Export constants
tool_caller.TOOL_TYPES = TOOL_TYPES

return tool_caller
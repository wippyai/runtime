local json = require("json")
local registry = require("registry")

-- Tool Resolver Library - For discovering tools and their schemas
local tool_resolver = {}

-- Fetch a tool's schema from the registry
function tool_resolver.get_tool_schema(tool_id)
    -- Get tool from registry directly
    local entry, err = registry.get(tool_id)
    if err or not entry then
        return nil, "Tool not found: " .. (err or "unknown error")
    end

    -- Validate that it's a tool
    if not entry.meta or entry.meta.type ~= "tool" then
        return nil, "Invalid tool type: " .. tool_id
    end

    -- Parse input schema
    local schema = nil
    if entry.meta.input_schema then
        schema, err = json.decode(entry.meta.input_schema)
        if err then
            return nil, "Invalid schema format: " .. err
        end
    end

    -- Create tool definition in generic format
    local tool = {
        id = tool_id,
        name = entry.meta.name or entry.id,
        description = entry.meta.description or "",
        schema = schema
    }

    return tool
end

-- Find tools by criteria (namespace, tags, etc.)
function tool_resolver.find_tools(criteria)
    local query = {
        [".kind"] = "function.lua",
        type = "tool"
    }

    -- Add additional criteria
    if criteria then
        if criteria.namespace then
            query["~namespace"] = criteria.namespace
        end

        if criteria.tags and #criteria.tags > 0 then
            query.meta.tags = criteria.tags
        end
    end

    -- Query registry directly
    local entries, err = registry.find(query)
    if err then
        return nil, "Failed to find tools: " .. err
    end

    if not entries or #entries == 0 then
        return {}
    end

    -- Convert to tool definitions
    local tools = {}
    for _, entry in ipairs(entries) do
        local tool, err = tool_resolver.get_tool_schema(entry.id)
        if tool then
            table.insert(tools, tool)
        end
    end

    return tools
end

-- Clear the tool cache
function tool_resolver.clear_cache()
    tool_cache = {}
    return true
end

return tool_resolver

local json = require("json")
local registry = require("registry")

-- Tool Resolver Library - For discovering tools and their schemas
local tool_resolver = {}

-- Sanitize tool name to avoid problematic characters for LLM and convert to snake_case
function tool_resolver.sanitize_name(name)
    -- Replace colons with underscores
    local sanitized = name:gsub(":", "_")

    -- Convert to snake_case
    -- Replace spaces and dashes with underscores
    sanitized = sanitized:gsub("%s+", "_"):gsub("-", "_")

    -- Convert camelCase or PascalCase to snake_case
    sanitized = sanitized:gsub("([A-Z])", function(c) return "_" .. c:lower() end)

    -- Fix double underscores and leading underscore if present
    sanitized = sanitized:gsub("__+", "_")
    sanitized = sanitized:gsub("^_", "")

    return sanitized
end

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

        -- Ensure there's at least one parameter for empty schemas
        if not schema.properties or next(schema.properties) == nil then
            schema.properties = {
                placeholder = {
                    type = "object",
                    description = "Empty placeholder parameter",
                    default = {}
                }
            }
        end
    else
        -- Create a default schema with placeholder if none exists
        schema = {
            type = "object",
            properties = {
                placeholder = {
                    type = "object",
                    description = "Empty placeholder parameter",
                    default = {}
                }
            }
        }
    end

    -- Create tool definition in generic format
    -- Use llm_alias if specified, otherwise use sanitized name
    local display_name = entry.meta.llm_alias or tool_resolver.sanitize_name(entry.meta.name or entry.id)

    -- Get description in priority order: llm_description > description > llm_descirtion > comment
    local description = ""
    if entry.meta then
        if entry.meta.llm_description then
            description = entry.meta.llm_description
        elseif entry.meta.description then
            description = entry.meta.description
        elseif entry.meta.llm_descirtion then -- Handle typo in field name
            description = entry.meta.llm_descirtion
        elseif entry.meta.comment then
            description = entry.meta.comment
        end
    end

    local tool = {
        id = tool_id,
        name = display_name,
        description = description,
        schema = schema
    }

    return tool
end

-- Get schemas for multiple tools by ID
function tool_resolver.get_tool_schemas(tool_ids)
    if not tool_ids or #tool_ids == 0 then
        return {}
    end

    local results = {}
    local errors = {}

    for _, id in ipairs(tool_ids) do
        local tool, err = tool_resolver.get_tool_schema(id)
        if tool then
            results[id] = tool
        else
            errors[id] = err
        end
    end

    return results, errors
end

-- Find a tool ID from a list of tool IDs that matches a given name
function tool_resolver.resolve_name_to_id(name, scope_ids)
    if not name or not scope_ids or #scope_ids == 0 then
        return nil, "Name and scope IDs are required"
    end

    -- Normalize the name for comparison
    local normalized_name = name:lower()

    -- Define match types in priority order
    local match_funcs = {
        -- 1. Exact llm_alias match
        function(entry, norm_name)
            return entry.meta.llm_alias and entry.meta.llm_alias:lower() == norm_name
        end,

        -- 2. Exact ID match
        function(entry, norm_name)
            return entry.id:lower() == norm_name
        end,

        -- 3. Exact name match
        function(entry, norm_name)
            return entry.meta.name and entry.meta.name:lower() == norm_name
        end,

        -- 4. Exact sanitized name match
        function(entry, norm_name)
            local sanitized = tool_resolver.sanitize_name(entry.meta.name or entry.id)
            return sanitized:lower() == norm_name
        end,

        -- 5. Partial llm_alias match
        function(entry, norm_name)
            return entry.meta.llm_alias and entry.meta.llm_alias:lower():find(norm_name, 1, true)
        end,

        -- 6. Partial ID match
        function(entry, norm_name)
            return entry.id:lower():find(norm_name, 1, true)
        end,

        -- 7. Partial name match
        function(entry, norm_name)
            return entry.meta.name and entry.meta.name:lower():find(norm_name, 1, true)
        end,

        -- 8. Partial sanitized name match
        function(entry, norm_name)
            local sanitized = tool_resolver.sanitize_name(entry.meta.name or entry.id)
            return sanitized:lower():find(norm_name, 1, true)
        end
    }

    -- Check each match type in order
    for _, match_func in ipairs(match_funcs) do
        for _, id in ipairs(scope_ids) do
            local entry, err = registry.get(id)
            if entry and entry.meta and entry.meta.type == "tool" then
                if match_func(entry, normalized_name) then
                    return id
                end
            end
        end
    end

    return nil, "No tool found with name: " .. name .. " in the provided scope"
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

return tool_resolver

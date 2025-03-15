local json = require("json")

-- Tool Resolver Library - For discovering tools and their schemas
local tool_resolver = {}

-- Allow for registry injection for testing
tool_resolver._registry = nil

-- Get registry - use injected registry or require it
local function get_registry()
    return tool_resolver._registry or require("registry")
end

-- Extract the tool name without namespace
local function extract_tool_name(full_id)
    -- Split by colon to separate namespace from tool name
    local parts = {}
    for part in string.gmatch(full_id, "[^:]+") do
        table.insert(parts, part)
    end

    -- If we have namespace:name format, return just the name part
    if #parts >= 2 then
        return parts[#parts]
    end

    -- Otherwise return the original id
    return full_id
end

-- Generate a sanitized name from any string input
local function sanitize_name(name)
    -- Replace non-allowed characters with underscores
    local sanitized = name:gsub("[^%w]", "_")

    -- Convert to snake_case
    sanitized = sanitized:gsub("([A-Z])", function(c) return "_" .. c:lower() end)

    -- Fix double underscores and leading underscore if present
    sanitized = sanitized:gsub("__+", "_")
    sanitized = sanitized:gsub("^_", "")

    return sanitized
end

-- Get the LLM-friendly tool name
function tool_resolver.get_tool_name(entry)
    -- If llm_alias is specified, use that
    if entry.meta and entry.meta.llm_alias then
        return entry.meta.llm_alias
    end

    -- Otherwise extract the name part from ID and sanitize
    local name = extract_tool_name(entry.id)
    return sanitize_name(name)
end

-- Fetch a tool's schema from the registry
function tool_resolver.get_tool_schema(tool_id)
    local registry = get_registry()

    -- Get tool from registry
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

    -- Get the tool name
    local display_name = tool_resolver.get_tool_name(entry)

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

    -- Process tools
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
    local registry = get_registry()

    if not name or not scope_ids or #scope_ids == 0 then
        return nil, "Name and scope IDs are required"
    end

    -- Normalize the name for comparison
    local normalized_name = name:lower()

    for _, id in ipairs(scope_ids) do
        local entry, err = registry.get(id)
        if not err and entry and entry.meta and entry.meta.type == "tool" then
            -- Try exact matches in priority order

            -- 1. Check generated tool name
            if tool_resolver.get_tool_name(entry):lower() == normalized_name then
                return id
            end

            -- 2. Check exact ID match
            if id:lower() == normalized_name then
                return id
            end

            -- 3. Check meta.name
            if entry.meta.name and entry.meta.name:lower() == normalized_name then
                return id
            end

            -- 4. Check sanitized meta.name
            if entry.meta.name and sanitize_name(entry.meta.name):lower() == normalized_name then
                return id
            end

            -- 5. Check extracted tool name part
            if extract_tool_name(id):lower() == normalized_name then
                return id
            end
        end
    end

    return nil, "No tool found with name: " .. name .. " in the provided scope"
end

-- Find tools by criteria (namespace, tags, etc.)
function tool_resolver.find_tools(criteria)
    local registry = get_registry()

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

    -- Query registry
    local entries, err = registry.find(query)
    if err then
        return nil, "Failed to find tools: " .. err
    end

    if not entries or #entries == 0 then
        return {}
    end

    -- Convert to tool definitions and check for duplicates
    local tools = {}
    local name_to_id = {}

    for _, entry in ipairs(entries) do
        local tool, err = tool_resolver.get_tool_schema(entry.id)
        if tool then
            -- Check for duplicate tool names
            if name_to_id[tool.name] then
                return nil, "Duplicate tool name detected: '" .. tool.name .. "' for tools '" ..
                           name_to_id[tool.name] .. "' and '" .. entry.id .. "'"
            end

            -- Record this tool name
            name_to_id[tool.name] = entry.id

            table.insert(tools, tool)
        end
    end

    -- Sort tools by name for stable ordering
    table.sort(tools, function(a, b)
        return a.name < b.name
    end)

    return tools
end

-- Backward compatibility function
function tool_resolver.sanitize_name(name)
    -- Handle the namespace case
    if name:find(":") then
        return sanitize_name(extract_tool_name(name))
    end
    return sanitize_name(name)
end

return tool_resolver
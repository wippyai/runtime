local registry = require("base_registry")

-- LLM Registry Library - For discovering LLM implementations
local llm_registry = {}

---------------------------
-- LLM Function Discovery
---------------------------

-- Implementation capability identifiers for registry lookups
llm_registry.CAPABILITY = {
    GENERATE = "generate",           -- Basic text generation
    TOOL_CALLING = "tools",          -- Text generation with tool calling
    GENERATE_WITH_SCHEMA = "schema", -- Schema-guided generation
    THINKING = "thinking"            -- Thinking/reasoning capability
}

-- Helper function to match a model name against a pattern
local function model_matches_pattern(model_name, pattern)
    if not pattern then return true end
    if pattern == "*" then return true end

    -- Convert glob-style patterns to Lua patterns
    local lua_pattern = pattern:gsub("%*", ".*"):gsub("%?", ".")
    return model_name:match("^" .. lua_pattern .. "$") ~= nil
end

-- Find an LLM implementation based on capability and model pattern
function llm_registry.find_implementation(capability, model)
    if not model then
        return nil, "Model is required"
    end

    local query = {
        [".kind"] = "function.lua",
        ["llm_function"] = capability
    }

    -- Query registry
    local entries, err = registry.find(query)
    if err then
        return nil, "Failed to find LLM implementations: " .. err
    end

    if not entries or #entries == 0 then
        return nil, "No LLM implementations found for capability: " .. capability
    end

    -- Filter entries by model patterns in metadata
    local matching_entries = {}

    for _, entry in ipairs(entries) do
        -- Check if entry has model patterns specified
        if not entry.meta.models then
            -- If entry doesn't have model patterns, include it as fallback
            table.insert(matching_entries, entry)
        else
            -- Check each model pattern in the entry
            for _, pattern in ipairs(entry.meta.models) do
                if model_matches_pattern(model, pattern) then
                    table.insert(matching_entries, entry)
                    break
                end
            end
        end
    end

    if #matching_entries == 0 then
        return nil, "No LLM implementations found for model: " .. model
    end

    -- Sort implementations by priority (if specified)
    table.sort(matching_entries, function(a, b)
        local a_priority = a.meta.priority or 0
        local b_priority = b.meta.priority or 0
        return a_priority > b_priority
    end)

    return matching_entries[1]
end

return llm_registry

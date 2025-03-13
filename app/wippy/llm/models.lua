-- Models Library - For discovering and managing LLM models
local registry = require("registry")

-- Main module
local models = {}

---------------------------
-- Constants
---------------------------

-- Model capability identifiers
models.CAPABILITY = {
    TOOL_USE = "tool_use",        -- Tool calling capability
    VISION = "vision",            -- Image/visual input capability
    THINKING = "thinking",        -- Extended thinking/reasoning
    CACHING = "caching",          -- Response caching capability
    MULTILINGUAL = "multilingual" -- Support for multiple languages
}

---------------------------
-- Model Discovery Functions
---------------------------

-- Find a model by its name (either display name or provider model)
function models.find_by_name(name)
    if not name then
        return nil, "Model name is required"
    end

    local snapshot, err = registry.snapshot()
    if err then
        return nil, "Failed to get registry snapshot: " .. err
    end

    -- Search for models with matching name or provider_model
    local entries = snapshot:find({
        [".kind"] = "registry.entry",
        ["meta.type"] = "llm.model",
        -- We'll sort and filter results programmatically
    })

    -- Find the exact match first
    for _, entry in ipairs(entries) do
        if entry.meta.name == name or entry.provider_model == name then
            return models._build_model_card(entry)
        end
    end

    -- Try case-insensitive matches
    local lower_name = name:lower()
    for _, entry in ipairs(entries) do
        if entry.meta.name:lower() == lower_name or
            (entry.provider_model and entry.provider_model:lower() == lower_name) then
            return models._build_model_card(entry)
        end
    end

    -- Try partial matches (contains)
    for _, entry in ipairs(entries) do
        if entry.meta.name:lower():find(lower_name, 1, true) or
            (entry.provider_model and entry.provider_model:lower():find(lower_name, 1, true)) then
            return models._build_model_card(entry)
        end
    end

    return nil, "No model found with name: " .. name
end

-- List all available models
function models.list_all()
    local snapshot, err = registry.snapshot()
    if err then
        return nil, "Failed to get registry snapshot: " .. err
    end

    local entries = snapshot:find({
        [".kind"] = "registry.entry",
        ["type"] = "llm.model"
    })

    if not entries or #entries == 0 then
        return {}, nil
    end

    -- Convert entries to model cards
    local result = {}
    for _, entry in ipairs(entries) do
        local model_card = models._build_model_card(entry)
        table.insert(result, model_card)
    end

    -- Sort by name for stable ordering
    table.sort(result, function(a, b)
        return a.name < b.name
    end)

    return result, nil
end

-- Find models by capability
function models.find_by_capability(capability)
    if not capability then
        return nil, "Capability is required"
    end

    local snapshot, err = registry.snapshot()
    if err then
        return nil, "Failed to get registry snapshot: " .. err
    end

    local entries = snapshot:find({
        [".kind"] = "registry.entry",
        type = "llm.model"
    })

    if not entries or #entries == 0 then
        return {}, nil
    end

    -- Filter entries by capability
    local result = {}
    for _, entry in ipairs(entries) do
        if entry.meta and entry.meta.capabilities then
            for _, cap in ipairs(entry.meta.capabilities) do
                if cap == capability then
                    local model_card = models._build_model_card(entry)
                    table.insert(result, model_card)
                    break
                end
            end
        end
    end

    -- Sort by name for stable ordering
    table.sort(result, function(a, b)
        return a.name < b.name
    end)

    return result, nil
end

-- Get model by provider model identifier
function models.get_by_provider_model(provider_model)
    if not provider_model then
        return nil, "Provider model ID is required"
    end

    local snapshot, err = registry.snapshot()
    if err then
        return nil, "Failed to get registry snapshot: " .. err
    end

    local entries = snapshot:find({
        [".kind"] = "registry.entry",
        type = "llm.model",
        provider_model = provider_model
    })

    if not entries or #entries == 0 then
        return nil, "No model found with provider ID: " .. provider_model
    end

    return models._build_model_card(entries[1])
end

-- Check if a model supports a specific capability
function models.has_capability(model_name, capability)
    local model, err = models.find_by_name(model_name)
    if not model then
        return false, err
    end

    if not model.capabilities then
        return false, nil
    end

    for _, cap in ipairs(model.capabilities) do
        if cap == capability then
            return true, nil
        end
    end

    return false, nil
end

---------------------------
-- Utility Functions
---------------------------

-- Build a model card from a registry entry
function models._build_model_card(entry)
    local model_card = {
        id = entry.id,
        name = entry.meta.name,
        provider_model = entry.provider_model,
        description = entry.meta.comment or "",
        capabilities = entry.meta.capabilities or {},
        max_tokens = entry.max_tokens,
        output_tokens = entry.output_tokens,
        pricing = entry.pricing,
        handlers = entry.handlers or {}
    }

    -- Add any additional metadata that might be useful
    if entry.meta.knowledge_cutoff then
        model_card.knowledge_cutoff = entry.meta.knowledge_cutoff
    end

    if entry.meta.mteb_performance then
        model_card.mteb_performance = entry.meta.mteb_performance
    end

    if entry.dimensions then
        model_card.dimensions = entry.dimensions
    end

    if entry.meta.model_family then
        model_card.model_family = entry.meta.model_family
    end

    return model_card
end

return models

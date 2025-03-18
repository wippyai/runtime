local registry = require("registry")
local traits = require("traits")

-- Main module
local agent_registry = {}

-- Allow for dependency injection for testing
agent_registry._registry = nil
agent_registry._traits = nil

-- Get registry - use injected registry or require it
local function get_registry()
    return agent_registry._registry or registry
end

-- Get traits - use injected traits or require it
local function get_traits()
    return agent_registry._traits or traits
end

---------------------------
-- Constants
---------------------------

-- Agent type identifier
agent_registry.AGENT_TYPE = "agent.gen1"

---------------------------
-- Agent Discovery Functions
---------------------------

-- Get agent specification by ID
function agent_registry.get_by_id(agent_id)
    if not agent_id then
        return nil, "Agent ID is required"
    end

    -- Get agent directly from registry without snapshot
    local reg = get_registry()
    local entry, err = reg.get(agent_id)
    if not entry then
        return nil, "No agent found with ID: " .. tostring(agent_id) .. ", error: " .. tostring(err)
    end

    -- Verify it's an agent
    if not entry.meta or entry.meta.type ~= agent_registry.AGENT_TYPE then
        return nil, "Entry is not a gen1 agent: " .. tostring(agent_id)
    end

    -- Build and return the full agent specification
    return agent_registry._build_agent_spec(entry)
end

-- Get agent specification by name
function agent_registry.get_by_name(name)
    if not name then
        return nil, "Agent name is required"
    end

    -- Find agents with matching name directly from registry
    local reg = get_registry()
    local entries = reg.find({
        [".kind"] = "registry.entry",
        ["meta.type"] = agent_registry.AGENT_TYPE,
        ["meta.name"] = name
    })

    if not entries or #entries == 0 then
        return nil, "No agent found with name: " .. name
    end

    -- Build and return the full agent specification for the first match
    return agent_registry._build_agent_spec(entries[1])
end

---------------------------
-- Utility Functions
---------------------------

-- Build a complete agent specification with resolved dependencies
function agent_registry._build_agent_spec(entry)
    -- Start with a complete agent spec from the entry data
    local agent_spec = {
        id = entry.id,
        name = (entry.meta and entry.meta.name) or "",
        description = (entry.meta and entry.meta.comment) or "",
        prompt = entry.data.prompt or "",
        model = entry.data.model or "",
        max_tokens = entry.data.max_tokens or 0,
        temperature = entry.data.temperature or 0,
        traits = entry.data.traits or {},
        tools = {},
        memory = {},
        handouts = {} -- Initialize handouts array
    }

    -- Copy the agent's own tools and memories first (to maintain order)
    if entry.data.tools then
        for _, tool in ipairs(entry.data.tools) do
            table.insert(agent_spec.tools, tool)
        end
    end

    if entry.data.memory then
        for _, mem in ipairs(entry.data.memory) do
            table.insert(agent_spec.memory, mem)
        end
    end

    -- Resolve parent agents if specified
    if entry.data.inherit and #entry.data.inherit > 0 then
        -- Process parent agents in order
        for _, parent_id in ipairs(entry.data.inherit) do
            local reg = get_registry()
            local parent_entry, err = reg.get(parent_id)
            if parent_entry and parent_entry.meta and parent_entry.meta.type == agent_registry.AGENT_TYPE then
                -- Add parent tools if they don't already exist
                if parent_entry.data.tools then
                    for _, tool in ipairs(parent_entry.data.tools) do
                        if not agent_registry._contains(agent_spec.tools, tool) then
                            table.insert(agent_spec.tools, tool)
                        end
                    end
                end

                -- Add parent memories if they don't already exist
                if parent_entry.data.memory then
                    for _, mem in ipairs(parent_entry.data.memory) do
                        if not agent_registry._contains(agent_spec.memory, mem) then
                            table.insert(agent_spec.memory, mem)
                        end
                    end
                end
            end
        end
    end

    -- Add handout tools and metadata if specified
    if entry.data.handout and #entry.data.handout > 0 then
        for _, handout_id in ipairs(entry.data.handout) do
            local reg = get_registry()
            local handout_entry, err = reg.get(handout_id)
            if handout_entry and handout_entry.meta and handout_entry.meta.type == agent_registry.AGENT_TYPE then
                -- Add handout tools if they don't already exist
                if handout_entry.data.tools then
                    for _, tool in ipairs(handout_entry.data.tools) do
                        if not agent_registry._contains(agent_spec.tools, tool) then
                            table.insert(agent_spec.tools, tool)
                        end
                    end
                end

                -- Add handout metadata to the handouts array
                table.insert(agent_spec.handouts, {
                    id = handout_entry.id,
                    name = (handout_entry.meta and handout_entry.meta.name) or "",
                    description = (handout_entry.meta and handout_entry.meta.comment) or ""
                })
            end
        end
    end

    -- Process traits and incorporate their prompts
    if entry.data.traits and #entry.data.traits > 0 then
        local trait_prompts = {}

        for _, trait_name in ipairs(entry.data.traits) do
            -- Try to get trait by name
            local traits_lib = get_traits()
            local trait, err = traits_lib.get_by_name(trait_name)
            if trait and trait.prompt and #trait.prompt > 0 then
                table.insert(trait_prompts, trait.prompt)
            end
        end

        -- Combine trait prompts with the agent's base prompt
        if #trait_prompts > 0 then
            -- Store the original trait prompts for reference
            agent_spec.trait_prompts = trait_prompts

            -- If the agent has a base prompt, combine it with trait prompts
            if agent_spec.prompt and #agent_spec.prompt > 0 then
                local combined_prompt = agent_spec.prompt
                for _, trait_prompt in ipairs(trait_prompts) do
                    combined_prompt = combined_prompt .. "\n\n" .. trait_prompt
                end
                agent_spec.prompt = combined_prompt
            else
                -- If no base prompt, just concatenate trait prompts
                agent_spec.prompt = table.concat(trait_prompts, "\n\n")
            end
        end
    end

    return agent_spec
end

-- Check if an array contains a value
function agent_registry._contains(array, value)
    for _, item in ipairs(array) do
        if item == value then
            return true
        end
    end
    return false
end

return agent_registry

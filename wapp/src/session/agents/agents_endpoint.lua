local http = require("http")
local json = require("json")
local security = require("security")
local start_tokens = require("start_tokens")

local function handler()
    -- Get response object
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Security check - ensure user is authenticated
    local actor = security.actor()
    if not actor then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Authentication required",
            details = "This endpoint requires valid authentication"
        })
        return
    end

    -- Import the registry
    local registry = require("registry")

    -- Find all agent entries in the registry
    local all_entries, err = registry.find({
        [".kind"] = "registry.entry",
        ["meta.type"] = "agent.gen1"
    })

    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = err
        })
        return
    end

    -- Filter metadata and transform entries
    local agents = {}
    for _, entry in ipairs(all_entries) do
        if entry.meta then
            -- Extract the model information
            local model = entry.meta.model or "gpt-4o-mini"

            -- Extract the session kind (default to "default" if not specified)
            local kind = entry.meta.session_kind or "default"

            -- Create the agent object with original metadata fields
            local agent = {
                name = entry.meta.name or "",
                title = entry.meta.title or entry.meta.name or "",
                group = entry.meta.group or {},
                icon = entry.meta.icon or "",
                tags = entry.meta.tags or {},
                model = model
            }

            -- Generate a start token for this agent
            local token_params = {
                agent = entry.meta.name or "",
                model = model,
                kind = kind
            }

            local token, token_err = start_tokens.pack(token_params)
            if token then
                agent.start_token = token
            else
                -- Log the error but continue without a token
                print("failed to generate start token for agent " .. agent.name .. ": " .. (token_err or "unknown error"))
                return
            end

            table.insert(agents, agent)
        end
    end

    -- Sort by group then title
    table.sort(agents, function(a, b)
        -- If both have group arrays, compare the first group
        if a.group and b.group and #a.group > 0 and #b.group > 0 then
            if a.group[1] ~= b.group[1] then
                return a.group[1] < b.group[1]
            end
        elseif a.group and #a.group > 0 then
            return true  -- a has group, b doesn't
        elseif b.group and #b.group > 0 then
            return false -- b has group, a doesn't
        end

        -- If groups are the same or not present, sort by title
        return a.title < b.title
    end)

    -- Group agents by group for easier client-side processing
    local grouped = {}
    for _, agent in ipairs(agents) do
        local groupName = "Ungrouped"
        if agent.group and #agent.group > 0 then
            groupName = agent.group[1]
        end

        if not grouped[groupName] then
            grouped[groupName] = {}
        end

        table.insert(grouped[groupName], agent)
    end

    -- Return JSON response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        count = #agents,
        agents = agents,
        grouped = grouped
    })
end

return {
    handler = handler
}

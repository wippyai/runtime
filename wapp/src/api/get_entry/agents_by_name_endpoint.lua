local http = require("http")
local json = require("json")
local registry = require("registry")

-- Helper function to split string
function string:split(sep)
    local fields = {}
    local pattern = string.format("([^%s]+)", sep)
    self:gsub(pattern, function(c) fields[#fields + 1] = c end)
    return fields
end

local function handler()
    -- Get response object
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Get query parameter with agent names
    local agents_param = req:query("agents")
    if not agents_param or agents_param == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:set_content_type(http.CONTENT.JSON)
        res:write_json({
            success = false,
            error = "Missing 'agents' query parameter. Expected comma-separated list of agent names."
        })
        return
    end

    -- Split comma-separated list of agent names
    local agent_names = agents_param:split(",")

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

    -- Create a lookup map of agent name to title
    local agent_map = {}
    for _, entry in ipairs(all_entries) do
        if entry.meta and entry.meta.name then
            agent_map[entry.meta.name] = entry.meta.title or entry.meta.name
        end
    end

    -- Map requested names to titles
    local result = {}
    for _, name in ipairs(agent_names) do
        result[name] = agent_map[name] or nil
    end

    -- Return JSON response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        agents = result
    })
end

return {
    handler = handler
}
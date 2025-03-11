local http = require("http")
local json = require("json")

local function handler()
    -- Get response object
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Import the tool_resolver library
    local tool_resolver = require("tools_reg")

    -- Get optional filter parameters
    local filter_namespace = req:query("namespace")
    local filter_query = req:query("q")

    -- Find all tools in the system
    local tools, err = tool_resolver.find_tools()

    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:set_content_type(http.CONTENT.JSON)
        res:write_json({
            error = "Failed to find tools",
            details = err
        })
        return
    end

    -- Apply filters if provided
    local filtered_tools = {}
    for _, tool in ipairs(tools) do
        local matches = true

        -- Filter by namespace
        if filter_namespace and filter_namespace ~= "" then
            local id_parts = tool.id:split(":")
            if id_parts[1] ~= filter_namespace then
                matches = false
            end
        end

        -- Filter by search query
        if filter_query and filter_query ~= "" then
            local query = filter_query:lower()
            local name_match = tool.name and tool.name:lower():find(query)
            local id_match = tool.id:lower():find(query)
            local desc_match = tool.description and tool.description:lower():find(query)

            if not (name_match or id_match or desc_match) then
                matches = false
            end
        end

        if matches then
            table.insert(filtered_tools, tool)
        end
    end

    -- Group tools by namespace
    local namespaces = {}
    for _, tool in ipairs(filtered_tools) do
        local id_parts = tool.id:split(":")
        local namespace = id_parts[1] or "default"

        if not namespaces[namespace] then
            namespaces[namespace] = {
                namespace = namespace,
                tools = {}
            }
        end

        table.insert(namespaces[namespace].tools, tool)
    end

    -- Convert to array and sort by namespace
    local grouped_namespaces = {}
    for _, group in pairs(namespaces) do
        table.insert(grouped_namespaces, group)
    end

    table.sort(grouped_namespaces, function(a, b)
        return a.namespace < b.namespace
    end)

    -- Sort tools within each namespace
    for _, group in ipairs(grouped_namespaces) do
        table.sort(group.tools, function(a, b)
            return a.name < b.name
        end)
    end

    -- Return JSON response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        count = #filtered_tools,
        filters = {
            namespace = filter_namespace,
            query = filter_query
        },
        tools = filtered_tools,
        grouped = grouped_namespaces
    })
end

-- Helper function to split string
function string:split(sep)
    local fields = {}
    local pattern = string.format("([^%s]+)", sep)
    self:gsub(pattern, function(c) fields[#fields + 1] = c end)
    return fields
end

return {
    handler = handler
}

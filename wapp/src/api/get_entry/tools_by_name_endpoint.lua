local http = require("http")
local json = require("json")
local tool_resolver = require("tools_reg")

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

    -- Get query parameter with tool names
    local tools_param = req:query("tools")
    if not tools_param or tools_param == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:set_content_type(http.CONTENT.JSON)
        res:write_json({
            success = false,
            error = "Missing 'tools' query parameter. Expected comma-separated list of tool IDs."
        })
        return
    end

    -- Split comma-separated list of tool IDs
    local tool_names = tools_param:split(",")

    -- Find all tools in the system
    local tools, err = tool_resolver.find_tools()

    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:set_content_type(http.CONTENT.JSON)
        res:write_json({
            success = false,
            error = "Failed to find tools",
            details = err
        })
        return
    end

    -- Create a lookup map of tool ID to name
    local tool_map = {}
    for _, tool in ipairs(tools) do
        tool_map[tool.name] = {
            name = tool.name,
            title = tool.title or tool.name,
        }
    end

    -- Map requested IDs to titles
    local result = {}
    for _, name in ipairs(tool_names) do
        result[name] = tool_map[name] or nil
    end

    -- Return JSON response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        tools = result
    })
end

return {
    handler = handler
}

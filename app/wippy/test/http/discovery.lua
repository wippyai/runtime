local http = require("http")
local json = require("json")
local time = require("time")
local registry = require("registry")

-- Function to discover tests without executing them
local function discover_tests()
    -- Set up HTTP response
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to create HTTP context"
    end

    -- Set response headers
    res:set_status(http.STATUS.OK)
    res:set_content_type(http.CONTENT.JSON)
    res:set_header("Access-Control-Allow-Origin", "*")
    res:set_header("Access-Control-Allow-Methods", "GET")

    -- Parse query parameters for filtering
    local options = {
        ["meta.type"] = "test"
    }

    if req:query("group") then
        options.group = req:query("group")
    end

    if req:query("tags") then
        options.tags = {}
        for tag in req:query("tags"):gmatch("([^,]+)") do
            table.insert(options.tags, tag:trim())
        end
    end

    -- Use test_registry to discover tests
    local tests, err = registry.find(options)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = err,
            timestamp = time.now():unix()
        })
        return false
    end

    -- Get all available groups
    local groups, err = registry.get_groups()
    if err then
        groups = {}
    end

    -- Return the discovered tests and groups
    res:write_json({
        tests = tests or {},
        groups = groups or {},
        timestamp = time.now():unix()
    })

    return true
end

return { discover_tests = discover_tests }

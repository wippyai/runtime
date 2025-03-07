local http = require("http")
local json = require("json")
local time = require("time")
local migration_registry = require("migration_registry")

-- Function to discover all target databases available for migrations
local function discover_dbs()
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

    -- Get all target databases
    local target_dbs = migration_registry.get_target_dbs()

    -- Return the discovered databases
    res:write_json({
        databases = target_dbs,
        count = #target_dbs,
        timestamp = time.now():unix()
    })

    return true
end

return { discover_dbs = discover_dbs }

local http = require("http")
local json = require("json")
local time = require("time")
local registry = require("registry")
local runner = require("runner")

-- Function to discover migrations with required target_db parameter
local function discover_migrations()
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

    -- Get target database from query parameters (required)
    local target_db = req:query("target_db")
    if not target_db or target_db == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing required parameter: target_db"
        })
        return true
    end

    -- Parse other query parameters for filtering
    local options = {
        target_db = target_db
    }

    -- Filter by tags
    if req:query("tags") then
        options.tags = {}
        for tag in req:query("tags"):gmatch("([^,]+)") do
            table.insert(options.tags, tag:trim())
        end
    end

    -- Setup a runner for the target database
    local db_runner = runner.setup(target_db)

    local status = db_runner:status(options)

    -- Check if status is an error response
    if status and status.status == "error" then
        res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
        res:write_json(status)
        return true
    end

    -- Add runtime info
    status.runtime = {
        requested_at = time.now():unix()
    }

    -- Return the migration status directly
    res:write_json(status)
    return true
end

-- Helper function to format migrations for response without 'method' field
function format_migrations(migrations)
    local formatted = {}

    for _, migration in ipairs(migrations) do
        -- Extract ID fields
        local id = "unknown"
        local name = "unknown"
        local namespace = "unknown"
        local kind = migration.kind or "unknown"

        if migration.id then
            if type(migration.id) == "table" then
                if migration.id.full then
                    id = migration.id.full
                elseif migration.id.ns and migration.id.name then
                    id = migration.id.ns .. ":" .. migration.id.name
                end

                name = migration.id.name or "unknown"
                namespace = migration.id.ns or "unknown"
            elseif type(migration.id) == "string" then
                id = migration.id
                local ns, n = migration.id:match("([^:]+):([^:]+)")
                if ns and n then
                    namespace = ns
                    name = n
                end
            end
        end

        -- Parse timestamp using time package
        local timestamp_str = (migration.meta and migration.meta.timestamp) or ""
        local timestamp = timestamp_str

        -- Try to parse timestamp if it's a string
        if type(timestamp_str) == "string" and #timestamp_str > 0 then
            -- Try RFC3339 format first
            local t, err = time.parse(time.RFC3339, timestamp_str)
            if t then
                timestamp = t:unix()
            else
                -- Try date-time format
                t, err = time.parse(time.DateTime, timestamp_str)
                if t then
                    timestamp = t:unix()
                end
            end
        end

        local entry = {
            id = id,
            name = name,
            namespace = namespace,
            kind = kind,
            description = (migration.meta and migration.meta.description) or "",
            target_db = (migration.meta and migration.meta.target_db) or "",
            timestamp = timestamp,
            timestamp_raw = timestamp_str,
            tags = (migration.meta and migration.meta.tags) or {}
        }

        table.insert(formatted, entry)
    end

    return formatted
end

return {
    discover_migrations = discover_migrations
}

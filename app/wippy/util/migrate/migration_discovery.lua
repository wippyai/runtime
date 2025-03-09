local http = require("http")
local json = require("json")
local time = require("time")
local migration_registry = require("migration_registry")

-- Function to discover migrations using migration_registry
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

    -- Parse query parameters for filtering
    local options = {}

    -- Filter by target_db instead of db_namespace
    if req:query("target_db") then
        options.target_db = req:query("target_db")
    end

    -- Filter by tags
    if req:query("tags") then
        options.tags = {}
        for tag in req:query("tags"):gmatch("([^,]+)") do
            table.insert(options.tags, tag:trim())
        end
    end

    -- Get migrations using migration_registry.find()
    local migrations = {}
    local find_err

    -- Use pcall to safely call migration_registry.find()
    migrations, find_err = migration_registry.find(options)
    if find_err then
        migrations = {}
    end

    -- Format migrations for response
    local formatted_migrations = format_migrations(migrations)

    -- Extract target databases
    local target_dbs = migration_registry.get_target_dbs()

    -- Return the discovered migrations and target databases
    res:write_json({
        migrations = formatted_migrations,
        databases = target_dbs,
        count = #formatted_migrations,
        timestamp = time.now():unix()
    })

    return true
end

-- Helper function to format migrations for response
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
            method = migration.method or ""
        }

        table.insert(formatted, entry)
    end

    return formatted
end

return { discover_migrations = discover_migrations }
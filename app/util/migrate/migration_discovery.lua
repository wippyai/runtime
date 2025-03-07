local http = require("http")
local json = require("json")
local time = require("time")
local migration_registry = require("migration_registry")
local registry = require("registry")

-- Function to discover migrations without executing them
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

    -- Filter by db_namespace
    if req:query("namespace") then
        options.db_namespace = req:query("namespace")
    end

    -- Filter by database type
    if req:query("db_type") then
        options.db_types = {}
        for db_type in req:query("db_type"):gmatch("([^,]+)") do
            table.insert(options.db_types, db_type:trim())
        end
    end

    -- Filter by tags
    if req:query("tags") then
        options.tags = {}
        for tag in req:query("tags"):gmatch("([^,]+)") do
            table.insert(options.tags, tag:trim())
        end
    end

    sdf.fff = ss.ll.dd()

print(":OK")
    -- Get registry snapshot directly for debugging
    local snapshot, snap_err = registry.snapshot()
    if snap_err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to get registry snapshot: " .. snap_err,
            timestamp = time.now():unix()
        })
        return false
    end

    -- Debug: Get all entries to see what's available
    local all_entries = snapshot:find({})

    -- Extract migration entries correctly
    local migrations = {}
    for _, entry in ipairs(all_entries) do
        if entry.kind == "migration.lua" then
            table.insert(migrations, entry)
        end
    end

    -- Get all available namespaces
    local namespaces = {}
    local namespace_set = {}

    for _, entry in ipairs(migrations) do
        if entry.meta and entry.meta.db_namespace then
            namespace_set[entry.meta.db_namespace] = true
        end
    end

    for ns in pairs(namespace_set) do
        table.insert(namespaces, ns)
    end

    -- Sort namespaces
    table.sort(namespaces)

    -- Format migrations for display
    local formatted_migrations = {}
    for _, migration in ipairs(migrations) do
        -- Apply filters if specified
        local include = true

        if options.db_namespace and
            (not migration.meta or migration.meta.db_namespace ~= options.db_namespace) then
            include = false
        end

        if include and options.db_types and #options.db_types > 0 then
            include = false
            if migration.meta and migration.meta.db_types then
                for _, db_type in ipairs(options.db_types) do
                    for _, mig_db_type in ipairs(migration.meta.db_types) do
                        if db_type == mig_db_type then
                            include = true
                            break
                        end
                    end
                    if include then break end
                end
            end
        end

        if include and options.tags and #options.tags > 0 then
            include = false
            if migration.meta and migration.meta.tags then
                for _, tag in ipairs(options.tags) do
                    for _, mig_tag in ipairs(migration.meta.tags) do
                        if tag == mig_tag then
                            include = true
                            break
                        end
                    end
                    if include then break end
                end
            end
        end

        if include then
            table.insert(formatted_migrations, {
                id = migration.id,
                description = (migration.meta and migration.meta.description) or "",
                db_namespace = (migration.meta and migration.meta.db_namespace) or "",
                db_types = (migration.meta and migration.meta.db_types) or {},
                tags = (migration.meta and migration.meta.tags) or {},
                timestamp = (migration.meta and migration.meta.timestamp) or ""
            })
        end
    end

    -- Debug information
    local debug_info = {
        total_registry_entries = #all_entries,
        migration_entries = #migrations,
        filtered_migrations = #formatted_migrations
    }

    -- Return the discovered migrations and namespaces
    res:write_json({
        migrations = formatted_migrations,
        namespaces = namespaces,
        count = #formatted_migrations,
        debug = debug_info,
        timestamp = time.now():unix()
    })

    return true
end

return { discover_migrations = discover_migrations }

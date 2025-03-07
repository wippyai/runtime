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

    -- Extract namespaces
    local namespaces = extract_namespaces(formatted_migrations)

    -- Return the discovered migrations and namespaces
    res:write_json({
        migrations = formatted_migrations,
        namespaces = namespaces,
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

        local entry = {
            id = id,
            name = name,
            namespace = namespace,
            kind = kind,
            description = (migration.meta and migration.meta.description) or "",
            db_namespace = (migration.meta and migration.meta.db_namespace) or "",
            db_types = (migration.meta and migration.meta.db_types) or {},
            timestamp = (migration.meta and migration.meta.timestamp) or "",
            method = migration.method or ""
        }

        table.insert(formatted, entry)
    end

    return formatted
end

-- Helper function to extract unique namespaces
function extract_namespaces(migrations)
    local namespace_set = {}

    for _, migration in ipairs(migrations) do
        if migration.db_namespace and migration.db_namespace ~= "" then
            namespace_set[migration.db_namespace] = true
        end
    end

    local namespaces = {}
    for ns in pairs(namespace_set) do
        table.insert(namespaces, ns)
    end

    table.sort(namespaces)
    return namespaces
end

return { discover_migrations = discover_migrations }

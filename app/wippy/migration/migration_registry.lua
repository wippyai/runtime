local registry = require("registry")

local migration_registry = {}

-- Find migrations in registry
function migration_registry.find(options)
    options = options or {}

    local criteria = {
        kind = "migration.lua"
    }

    if options.db_namespace then
        criteria.meta = criteria.meta or {}
        criteria.meta.db_namespace = options.db_namespace
    end

    if options.db_types and #options.db_types > 0 then
        criteria.meta = criteria.meta or {}
        criteria.meta.db_types = options.db_types
    end

    if options.tags and #options.tags > 0 then
        criteria.meta = criteria.meta or {}
        criteria.meta.tags = options.tags
    end

    -- Get registry snapshot
    local snapshot, err = registry.snapshot()
    if err then
        return nil, "Failed to get registry snapshot: " .. err
    end

    -- Find migrations
    local entries = snapshot:find(criteria)

    -- Sort by timestamp
    table.sort(entries, function(a, b)
        local a_time = a.meta and a.meta.timestamp or ""
        local b_time = b.meta and b.meta.timestamp or ""
        return a_time < b_time
    end)

    return entries
end

-- Get specific migration by ID
function migration_registry.get(id)
    local snapshot, err = registry.snapshot()
    if err then
        return nil, "Failed to get registry snapshot: " .. err
    end

    return snapshot:get(id)
end

-- Get entries for a specific db_namespace
function migration_registry.get_by_namespace(db_namespace)
    if not db_namespace then
        return nil, "db_namespace is required"
    end

    local snapshot, err = registry.snapshot()
    if err then
        return nil, "Failed to get registry snapshot: " .. err
    end

    local entries = snapshot:find({
        kind = "migration.lua",
        meta = {
            db_namespace = db_namespace
        }
    })

    -- Sort by timestamp
    table.sort(entries, function(a, b)
        local a_time = a.meta and a.meta.timestamp or ""
        local b_time = b.meta and b.meta.timestamp or ""
        return a_time < b_time
    end)

    return entries
end

-- Get all migration namespaces
function migration_registry.get_namespaces()
    local snapshot, err = registry.snapshot()
    if err then
        return nil, "Failed to get registry snapshot: " .. err
    end

    local entries = snapshot:find({
        kind = "migration.lua"
    })

    local namespaces = {}
    for _, entry in ipairs(entries) do
        if entry.meta and entry.meta.db_namespace then
            namespaces[entry.meta.db_namespace] = true
        end
    end

    local result = {}
    for namespace in pairs(namespaces) do
        table.insert(result, namespace)
    end

    table.sort(result)
    return result
end

return migration_registry

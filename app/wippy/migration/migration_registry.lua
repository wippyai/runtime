local time = require("time")
local migrations = {
    registry = require("registry")
}

-- Find migrations in registry
function migrations.find(options)
    options = options or {}

    local criteria = {
        [".kind"] = "function.lua",
        type = "migration",
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

    -- Find migrations
    local entries = migrations.registry.find(criteria)

    -- Sort by timestamp
    table.sort(entries, function(a, b)
        local a_time = a.meta and a.meta.timestamp or ""
        local b_time = b.meta and b.meta.timestamp or ""
        return a_time < b_time
    end)

    return entries
end

-- Get specific migration by ID
function migrations.get(id)
    return migrations.registry.get(id)
end

-- Get entries for a specific db_namespace
function migrations.get_by_namespace(db_namespace)
    if not db_namespace then
        return nil, "db_namespace is required"
    end

    local entries = migrations.registry.find({
        [".kind"] = "migration.lua",
        db_namespace = db_namespace
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
function migrations.get_namespaces()
    local entries = migrations.registry.find({
        [".kind"] = "function.lua",
        type = "migration",
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

return migrations

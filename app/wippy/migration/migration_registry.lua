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

    -- Use target_db instead of db_namespace and db_types
    if options.target_db then
        criteria.meta = criteria.meta or {}
        criteria.meta.target_db = options.target_db
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

-- Get all target databases used in migrations
function migrations.get_target_dbs()
    local entries = migrations.registry.find({
        [".kind"] = "function.lua",
        type = "migration",
    })

    local target_dbs = {}
    for _, entry in ipairs(entries) do
        if entry.meta and entry.meta.target_db then
            target_dbs[entry.meta.target_db] = true
        end
    end

    local result = {}
    for db in pairs(target_dbs) do
        table.insert(result, db)
    end

    table.sort(result)
    return result
end

return migrations
local migration_schema = {}
local sql = require("sql")
local json = require("json")

-- Schema definitions for tracking table by database type
migration_schema.schemas = {
    [sql.type.postgres] = [[
        CREATE TABLE IF NOT EXISTS _migrations (
            id VARCHAR(255) PRIMARY KEY,
            db_namespace VARCHAR(255) NOT NULL,
            applied_at TIMESTAMP NOT NULL DEFAULT NOW(),
            description TEXT,
            status VARCHAR(50) NOT NULL,
            duration FLOAT,
            details JSONB
        )
    ]],

    [sql.type.sqlite] = [[
        CREATE TABLE IF NOT EXISTS _migrations (
            id TEXT PRIMARY KEY,
            db_namespace TEXT NOT NULL,
            applied_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
            description TEXT,
            status TEXT NOT NULL,
            duration REAL,
            details TEXT
        )
    ]],

    [sql.type.mysql] = [[
        CREATE TABLE IF NOT EXISTS _migrations (
            id VARCHAR(255) PRIMARY KEY,
            db_namespace VARCHAR(255) NOT NULL,
            applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            description TEXT,
            status VARCHAR(50) NOT NULL,
            duration FLOAT,
            details JSON
        )
    ]]
}

-- Initialize migration tracking table
function migration_schema.init_tracking_table(db)
    local db_type, err = db:type()
    if err then
        return nil, "Failed to determine database type: " .. err
    end

    local schema = migration_schema.schemas[db_type]
    if not schema then
        return nil, "Unsupported database type: " .. db_type
    end

    return db:execute(schema)
end

-- Record migration execution
function migration_schema.record_migration(db, id, db_namespace, description, status, duration, details)
    local db_type, err = db:type()
    if err then
        return nil, "Failed to determine database type: " .. err
    end

    local query
    local params

    -- Format details as JSON string for storage
    local details_json = nil
    if details then
        local encoded, encode_err = json.encode(details)
        if not encode_err then
            details_json = encoded
        end
    end

    query = [[
        INSERT INTO _migrations (id, db_namespace, description, status, duration, details)
        VALUES (?, ?, ?, ?, ?, ?)
    ]]
    params = { id, db_namespace, description, status, duration, details_json }

    return db:execute(query, params)
end

-- Update migration duration after transaction commits
function migration_schema.update_migration_duration(db, id, duration)
    local query = [[
        UPDATE _migrations
        SET duration = ?
        WHERE id = ?
    ]]

    return db:execute(query, { duration, id })
end

-- Get migrations by filter
function migration_schema.get_migrations(db, filter)
    filter = filter or {}

    local query = "SELECT id, db_namespace, applied_at, description, status, duration, details FROM _migrations"
    local params = {}
    local where_clauses = {}

    if filter.db_namespace then
        table.insert(where_clauses, "db_namespace = ?")
        table.insert(params, filter.db_namespace)
    end

    if filter.status then
        table.insert(where_clauses, "status = ?")
        table.insert(params, filter.status)
    end

    if filter.id then
        table.insert(where_clauses, "id = ?")
        table.insert(params, filter.id)
    end

    if #where_clauses > 0 then
        query = query .. " WHERE " .. table.concat(where_clauses, " AND ")
    end

    query = query .. " ORDER BY applied_at"

    local results, err = db:query(query, params)
    if err then
        return nil, err
    end

    -- Parse details JSON if available
    for i, row in ipairs(results) do
        if row.details and type(row.details) == "string" then
            local parsed, parse_err = json.decode(row.details)
            if not parse_err then
                results[i].details = parsed
            end
        end
    end

    return results
end

return migration_schema

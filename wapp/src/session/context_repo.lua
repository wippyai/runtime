local sql = require("sql")

-- Hardcoded database resource name
local DB_RESOURCE = "app:db"

local context_repo = {}

-- Get a database connection
local function get_db()
    local db, err = sql.get(DB_RESOURCE)
    if err then
        return nil, "Failed to connect to database: " .. err
    end
    return db
end

-- Create a new context
function context_repo.create(context_id, type, data)
    if not context_id or context_id == "" then
        return nil, "Context ID is required"
    end

    if not type or type == "" then
        return nil, "Context type is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local result, err = db:execute(
        "INSERT INTO contexts (context_id, type, data) VALUES (?, ?, ?)",
        { context_id, type, data }
    )

    db:release()

    if err then
        return nil, "Failed to create context: " .. err
    end

    return {
        context_id = context_id,
        type = type
    }
end

-- Get a context by ID
function context_repo.get(context_id)
    if not context_id or context_id == "" then
        return nil, "Context ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT context_id, type, data
        FROM contexts
        WHERE context_id = ?
    ]]

    local contexts, err = db:query(query, { context_id })
    db:release()

    if err then
        return nil, "Failed to get context: " .. err
    end

    if #contexts == 0 then
        return nil, "Context not found"
    end

    return contexts[1]
end

-- Update context data
function context_repo.update(context_id, data)
    if not context_id or context_id == "" then
        return nil, "Context ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Check if context exists
    local contexts, err = db:query("SELECT context_id FROM contexts WHERE context_id = ?", { context_id })
    if err then
        db:release()
        return nil, "Failed to check if context exists: " .. err
    end

    if #contexts == 0 then
        db:release()
        return nil, "Context not found"
    end

    -- Update context data
    local result, err = db:execute(
        "UPDATE contexts SET data = ? WHERE context_id = ?",
        { data, context_id }
    )

    db:release()

    if err then
        return nil, "Failed to update context: " .. err
    end

    return {
        context_id = context_id,
        updated = true
    }
end

-- Get contexts by type
function context_repo.get_by_type(type, limit, offset)
    if not type or type == "" then
        return nil, "Context type is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT context_id, type, data
        FROM contexts
        WHERE type = ?
    ]]

    -- Add limit and offset if provided
    if limit and limit > 0 then
        query = query .. " LIMIT " .. limit
        if offset and offset > 0 then
            query = query .. " OFFSET " .. offset
        end
    end

    local contexts, err = db:query(query, { type })
    db:release()

    if err then
        return nil, "Failed to get contexts by type: " .. err
    end

    return contexts
end

-- Delete a context
function context_repo.delete(context_id)
    if not context_id or context_id == "" then
        return nil, "Context ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Check if context exists
    local contexts, err = db:query("SELECT context_id FROM contexts WHERE context_id = ?", { context_id })
    if err then
        db:release()
        return nil, "Failed to check if context exists: " .. err
    end

    if #contexts == 0 then
        db:release()
        return nil, "Context not found"
    end

    -- Delete the context
    local result, err = db:execute("DELETE FROM contexts WHERE context_id = ?", { context_id })

    db:release()

    if err then
        return nil, "Failed to delete context: " .. err
    end

    return { deleted = true }
end

return context_repo

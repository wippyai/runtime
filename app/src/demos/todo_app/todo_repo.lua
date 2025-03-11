local sql = require("sql")

-- Hardcoded database resource name
local DB_RESOURCE = "system:db"

local todo_repo = {}

-- Get a database connection
local function get_db()
    local db, err = sql.get(DB_RESOURCE)
    if err then
        return nil, "Failed to connect to database: " .. err
    end
    return db
end

-- Create a new todo
function todo_repo.add(title, note)
    if not title or title == "" then
        return nil, "Title is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        INSERT INTO todos (title, note, created_at, updated_at)
        VALUES (?, ?, strftime('%s', 'now'), strftime('%s', 'now'))
    ]]

    local result, err = db:execute(query, {title, note or ""})
    db:release()

    if err then
        return nil, "Failed to create todo: " .. err
    end

    -- Return the ID of the newly created todo
    return {id = result.last_insert_id}
end

-- Get a single todo by ID
function todo_repo.get(id)
    if not id or id <= 0 then
        return nil, "Invalid todo ID"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT id, title, note, created_at, updated_at
        FROM todos
        WHERE id = ?
    ]]

    local todos, err = db:query(query, {id})
    db:release()

    if err then
        return nil, "Failed to get todo: " .. err
    end

    if #todos == 0 then
        return nil, "Todo not found"
    end

    return todos[1]
end

-- Get all todos
function todo_repo.list()
    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT id, title, note, created_at, updated_at
        FROM todos
        ORDER BY created_at DESC
    ]]

    local todos, err = db:query(query)
    db:release()

    if err then
        return nil, "Failed to list todos: " .. err
    end

    return todos
end

-- Update a todo
function todo_repo.update(id, title, note)
    if not id or id <= 0 then
        return nil, "Invalid todo ID"
    end

    if not title or title == "" then
        return nil, "Title is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        UPDATE todos
        SET title = ?, note = ?, updated_at = strftime('%s', 'now')
        WHERE id = ?
    ]]

    local result, err = db:execute(query, {title, note or "", id})
    db:release()

    if err then
        return nil, "Failed to update todo: " .. err
    end

    if result.rows_affected == 0 then
        return nil, "Todo not found"
    end

    return {id = id}
end

-- Delete a todo
function todo_repo.delete(id)
    if not id or id <= 0 then
        return nil, "Invalid todo ID"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        DELETE FROM todos
        WHERE id = ?
    ]]

    local result, err = db:execute(query, {id})
    db:release()

    if err then
        return nil, "Failed to delete todo: " .. err
    end

    if result.rows_affected == 0 then
        return nil, "Todo not found"
    end

    return {success = true}
end

return todo_repo
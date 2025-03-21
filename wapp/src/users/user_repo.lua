local sql = require("sql")

-- Hardcoded database resource name
local DB_RESOURCE = "app:db"

local user_repo = {}

-- Get a database connection
local function get_db()
    local db, err = sql.get(DB_RESOURCE)
    if err then
        return nil, "Failed to connect to database: " .. err
    end
    return db
end

-- Upsert a user (create if not exists, update if exists)
function user_repo.upsert(user_id)
    if not user_id or user_id == "" then
        return nil, "User ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Update last_login timestamp
    local now = os.time()

    -- Check if user exists
    local users, err = db:query("SELECT user_id FROM users WHERE user_id = ?", { user_id })
    if err then
        db:release()
        return nil, "Failed to check if user exists: " .. err
    end

    local result, err
    if #users == 0 then
        -- Create new user
        result, err = db:execute(
            "INSERT INTO users (user_id, last_login, created_at) VALUES (?, ?, strftime('%s', 'now'))",
            { user_id, now }
        )
    else
        -- Update existing user
        result, err = db:execute(
            "UPDATE users SET last_login = ? WHERE user_id = ?",
            { now, user_id }
        )
    end

    db:release()

    if err then
        return nil, "Failed to upsert user: " .. err
    end

    return { user_id = user_id, last_login = now }
end

-- Get a user by ID
function user_repo.get(user_id)
    if not user_id or user_id == "" then
        return nil, "User ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT user_id, last_login, created_at
        FROM users
        WHERE user_id = ?
    ]]

    local users, err = db:query(query, { user_id })
    db:release()

    if err then
        return nil, "Failed to get user: " .. err
    end

    if #users == 0 then
        return nil, "User not found"
    end

    return users[1]
end

-- Get all users
function user_repo.list()
    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT user_id, last_login, created_at
        FROM users
        ORDER BY created_at DESC
    ]]

    local users, err = db:query(query)
    db:release()

    if err then
        return nil, "Failed to list users: " .. err
    end

    return users
end

return user_repo

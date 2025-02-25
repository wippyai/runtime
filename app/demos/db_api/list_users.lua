local http = require("http")
local sql = require("sql")
local json = require("json")

-- List all users from the database
function list_users()
    -- Set up response
    local res = http.response()
    local req = http.request()
    res:set_content_type(http.CONTENT.JSON)

    -- Get database connection
    local db, err = sql.get("system:db")
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Database connection failed: " .. err
        })
        return
    end

    -- Prepare SQL query
    local query = [[
    SELECT id, username, email, created_at
    FROM users
    ORDER BY id DESC
    ]]

    -- Execute query
    local users, err = db:query(query)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to query users: " .. err
        })
        db:release()
        return
    end

    -- Format timestamps for better readability
    for _, user in ipairs(users) do
        user.created_at = tostring(user.created_at)
    end

    -- Release database connection
    db:release()

    -- Return success response
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        count = #users,
        users = users
    })
end

return {
    list_users = list_users
}
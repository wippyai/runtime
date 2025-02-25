local http = require("http")
local sql = require("sql")
local json = require("json")

-- Setup users table in the database
function setup_users_table()
    -- Set up response
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)

    -- Get database connection from the system resource
    local db, err = sql.get("system:db")
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Database connection failed: " .. err
        })
        return
    end

    -- Create users table
    local create_table_sql = [[
     CREATE TABLE IF NOT EXISTS users (
         id INTEGER PRIMARY KEY AUTOINCREMENT,
         username TEXT NOT NULL UNIQUE,
         email TEXT NOT NULL,
         created_at INTEGER NOT NULL
     )
     ]]

    local result, err = db:execute(create_table_sql)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to create users table: " .. err
        })
        db:release()
        return
    end

    -- Release database connection
    db:release()

    -- Return success response
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        message = "Users table created or already exists"
    })
end

return {
    setup_users_table = setup_users_table
}

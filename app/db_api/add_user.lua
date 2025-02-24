local http = require("http")
local sql = require("sql")
local json = require("json")
local time = require("time")

-- Add a new user to the database
function add_user()
    -- Set up response
    local res = http.response()
    local req = http.request()
    res:set_content_type(http.CONTENT.JSON)

    -- Parse JSON from request body
    local body = req:body()
    local user_data, err = json.decode(body)

    if err or not user_data then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Invalid JSON in request body"
        })
        return
    end

    -- Validate required fields
    if not user_data.username or not user_data.email then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing required fields: username and email"
        })
        return
    end

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

    -- Prepare insert statement
    local insert_sql = [[
    INSERT INTO users (username, email, created_at)
    VALUES (?, ?, ?)
    ]]

    -- Get current timestamp
    local current_time = time.now():unix()

    -- Execute insert
    local result, err = db:execute(insert_sql, {
        user_data.username,
        user_data.email,
        current_time
    })

    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)

        -- Check for unique constraint violation
        if err:find("UNIQUE constraint failed") then
            res:set_status(http.STATUS.CONFLICT)
            res:write_json({
                success = false,
                error = "Username or email already exists"
            })
        else
            res:write_json({
                success = false,
                error = "Failed to add user: " .. err
            })
        end

        db:release()
        return
    end

    -- Get the ID of the inserted user
    local last_id = result.last_insert_id

    -- Release database connection
    db:release()

    -- Return success response
    res:set_status(http.STATUS.CREATED)
    res:write_json({
        success = true,
        message = "User created successfully",
        user_id = last_id
    })
end

return {
    add_user = add_user
}
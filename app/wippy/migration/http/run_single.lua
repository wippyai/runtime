local http = require("http")
local json = require("json")
local time = require("time")
local runner = require("runner")

-- Function to run a specific migration
local function run_migration()
    -- Set up HTTP response
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to create HTTP context"
    end

    -- Set response headers
    res:set_status(http.STATUS.OK)
    res:set_content_type(http.CONTENT.JSON)
    res:set_header("Access-Control-Allow-Origin", "*")
    res:set_header("Access-Control-Allow-Methods", "POST")

    -- Only accept POST requests
    if req:method() ~= "POST" then
        res:set_status(http.STATUS.METHOD_NOT_ALLOWED)
        res:write_json({
            error = "Only POST method is allowed for this endpoint"
        })
        return true
    end

    -- Parse request body
    local body, parse_err = req:body_json()
    if parse_err or not body then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Invalid JSON body: " .. (parse_err or "unknown error")
        })
        return true
    end

    -- Validate required parameters
    if not body.target_db or body.target_db == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing required parameter: target_db"
        })
        return true
    end

    if not body.migration_id or body.migration_id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing required parameter: migration_id"
        })
        return true
    end

    -- Setup runner for the target database
    local db_runner = runner.setup(body.target_db)

    -- Determine migration direction (up or down)
    local direction = (body.direction and body.direction:lower() == "down") and "down" or "up"

    -- Set up options
    local options = {
        id = body.migration_id,
        force = body.force == true,
        dry_run = body.dry_run == true
    }

    -- Run the migration
    local start_time = time.now()
    local result

    if direction == "up" then
        result = db_runner:run_next(options)
    else
        result = db_runner:rollback(options)
    end

    -- Check if result is an error response
    if result and result.status == "error" then
        res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
        res:write_json(result)
        return true
    end

    local end_time = time.now()
    local duration = end_time:sub(start_time):milliseconds() / 1000 -- In seconds

    -- Enhance the result with runtime info
    result.runtime = {
        requested_at = start_time:unix(),
        duration = duration,
        direction = direction
    }

    -- Return the result
    res:write_json(result)

    return true
end

return { run_migration = run_migration }
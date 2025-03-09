local http = require("http")
local json = require("json")
local time = require("time")
local runner = require("runner")

-- Function to rollback migrations
local function rollback_migrations()
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

    -- Setup runner for the target database
    local db_runner = runner.setup(body.target_db)

    -- Set up options
    local options = {
        dry_run = body.dry_run == true
    }

    -- Add count if specified
    if body.count and type(body.count) == "number" and body.count > 0 then
        options.count = body.count
    end

    -- Run the rollback
    local start_time = time.now()
    local result = db_runner:rollback(options)

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
        duration = duration
    }

    -- Return the result
    res:write_json(result)

    return true
end

return { rollback_migrations = rollback_migrations }

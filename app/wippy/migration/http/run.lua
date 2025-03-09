local http = require("http")
local json = require("json")
local time = require("time")
local runner = require("runner")

-- Function to run all pending migrations
local function run_migrations()
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
    local db_runner
    local setup_success, setup_err = pcall(function()
        db_runner = runner.setup(body.target_db)
    end)

    if not setup_success or not db_runner then
        res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
        res:write_json({
            error = "Failed to setup migration runner: " .. (setup_err or "unknown error")
        })
        return true
    end

    -- Set up options
    local options = {
        force = body.force == true,
        dry_run = body.dry_run == true
    }

    -- Add tags if specified
    if body.tags and type(body.tags) == "table" and #body.tags > 0 then
        options.tags = body.tags
    end

    -- Run the migrations
    local start_time = time.now()
    local result = db_runner:run(options)
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

return { run_migrations = run_migrations }
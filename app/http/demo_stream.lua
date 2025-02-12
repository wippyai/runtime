local json = require("json")
local http = require("http")
local time = require("time")
local logger = require("logger")

--- Stream demonstration handler that supports multiple formats
-- @module demo_stream
local M = {}

function M.handler()
    local res = http.response()
    local req = http.request()

    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Get request parameters with defaults
    local format = req:query("format") or "text"
    local duration_str = req:query("duration") or "10s"
    local interval_str = req:query("interval") or "1s"

    -- Parse duration and interval
    local duration = time.parse_duration(duration_str)
    local interval = time.parse_duration(interval_str)

    if not duration or not interval then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Invalid duration or interval format. Use format like '10s', '1m', etc."
        })
        return
    end

    -- Setup response based on format
    if format == "text" then
        res:set_content_type(http.CONTENT.TEXT)
        res:set_transfer(http.TRANSFER.CHUNKED)
    elseif format == "json" then
        res:set_content_type(http.CONTENT.JSON)
        res:set_transfer(http.TRANSFER.CHUNKED)
    elseif format == "sse" then
        res:set_transfer(http.TRANSFER.SSE)
    else
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Invalid format. Supported formats: text, json, sse"
        })
        return
    end

    -- Start streaming
    local start_time = time.now()
    local count = 0
    local end_time = start_time:add(duration)

    while time.now():before(end_time) do
        count = count + 1
        local elapsed = time.now():sub(start_time)

        -- Stream data in requested format
        if format == "text" then
            res:write(string.format("Update #%d - Time elapsed: %s\n", count, tostring(elapsed)))
        elseif format == "json" then
            local data = {
                update = count,
                elapsed = tostring(elapsed),
                timestamp = time.now():unix()
            }
            res:write(json.encode(data) .. "\n")
        elseif format == "sse" then
            res:write_event({
                name = "update",
                data = {
                    count = count,
                    elapsed = tostring(elapsed),
                    timestamp = time.now():unix()
                }
            })
        end

        res:flush()

        -- Sleep for the interval
        time.sleep(interval)
    end

    -- Send final message
    if format == "text" then
        res:write("Stream completed after " .. count .. " updates\n")
    elseif format == "json" then
        res:write(json.encode({
            status = "completed",
            total_updates = count,
            total_duration = tostring(duration)
        }) .. "\n")
    elseif format == "sse" then
        res:write_event({
            name = "complete",
            data = {
                total_updates = count,
                total_duration = tostring(duration)
            }
        })
    end

    res:flush()
end

return M
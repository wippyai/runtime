local time = require("time")
local http = require("http")

local function handler()
    -- Set up response
    local res = http.response()
    if not res then
        return nil, "Failed to create HTTP response"
    end

    res:set_content_type(http.CONTENT.JSON)

    -- Get current time in local timezone
    local current_time = time.now()
    if not current_time then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to get current time"
        })
        return
    end

    -- Convert to local timezone
    local local_time = current_time:in_local()
    if not local_time then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to convert to local timezone"
        })
        return
    end

    -- Get timezone location
    local location = local_time:location()
    local timezone = "unknown"
    if location then
        timezone = tostring(location)
    end

    -- Format time using RFC3339
    local formatted_time = local_time:format_rfc3339()
    if not formatted_time then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to format time"
        })
        return
    end

    -- Get components for additional info
    local year, month, day = local_time:date()
    local hour, min, sec = local_time:clock()

    -- Success response
    res:set_status(http.STATUS.OK)
    res:write_json({
        time = formatted_time,
        unix_timestamp = local_time:unix(),
        timezone = timezone,
        components = {
            year = year,
            month = month,
            day = day,
            hour = hour,
            minute = min,
            second = sec,
            weekday = local_time:weekday()
        }
    })
end

-- Export the function
return {
    handler = handler
}

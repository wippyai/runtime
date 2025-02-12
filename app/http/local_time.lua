local time = require("time")
local http = require("http")

function get_time()
    -- Set up response
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)

    -- Get current time
    local current_time = time.now()
    if not current_time then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to get current time"
        })
        return
    end

    -- Format time and create response
    local formatted_time, format_err = current_time:format(time.RFC3339)
    if format_err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to format time",
            details = format_err
        })
        return
    end

    -- Success response
    res:set_status(http.STATUS.OK)
    res:write_json({
        time = formatted_time,
        unix_timestamp = current_time:unix(),
        timezone = current_time:timezone()
    })
end

local http_client = require("http_client")
local http = require("http")
local json = require("json")

function get_time()
    -- Set up response
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)

    local api_url = "http://worldtimeapi.org/api/timezone/Etc/UTC"

    -- Make the HTTP GET request with proper options
    local response, err = http_client.get(api_url, {
        timeout = "5s",
        headers = {
            ["User-Agent"] = "Lua HTTP Client"
        }
    })

    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to fetch time",
            details = err
        })
        return
    end

    if response.status_code ~= http.STATUS.OK then
        res:set_status(response.status_code)
        res:write_json({
            error = "Time service error",
            status = response.status_code,
            body = response.body
        })
        return
    end

    -- Decode the JSON response
    local data, decode_err = json.decode(response.body)
    if decode_err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to decode response",
            details = decode_err
        })
        return
    end

    -- Extract and validate the datetime
    local current_time = data.datetime
    if not current_time then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Missing datetime in response",
            response = data
        })
        return
    end

    -- Success response
    res:set_status(http.STATUS.OK)
    res:write_json({
        current_time = current_time,
        timezone = "UTC",
        timestamp = data.unixtime
    })
end

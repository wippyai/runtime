local http = require("http")
local json = require("json")

local function main()
    local api_url = "http://worldtimeapi.org/api/timezone/Etc/UTC"

    -- Make the HTTP GET request
    local response, err = http.get(api_url)
    if err then
        return { error = "Failed to fetch time: " .. err }
    end

    if response.status_code ~= 200 then
        return { error = "Time service returned supervisor code: " .. response.status_code }
    end

    -- Decode the JSON response
    local data, decode_err = json.decode(response.body)
    if not data then
        return { error = "Failed to decode JSON response: " .. (decode_err or "unknown error") }
    end

    -- Extract the current datetime
    local current_time = data.datetime
    if not current_time then
        return { error = "Current time not found in the response." }
    end

    return { current_time = current_time }
end

return main

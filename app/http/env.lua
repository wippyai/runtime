local json = require("json")
local http = require("http")
local env = require("env")

function envdump()
    -- Get request context and set up response
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)
    res:set_transfer(http.TRANSFER.CHUNKED)

    -- Create a channel for communication
    local env_channel = channel.new(1)

    -- Spawn producer coroutine
    coroutine.spawn(function()
        -- Get all environment variables
        local vars = env.get_all()

        -- Send specific important vars first
        local important = {
            path = env.get("PATH"),
            home = env.get("HOME"),
            user = env.get("USER"),
            pwd = env.get("PWD")
        }

        -- Check for channel send success
        local ok = env_channel:send({
            type = "important",
            vars = important
        })

        if not ok then
            return
        end

        -- Send all variables
        ok = env_channel:send({
            type = "all",
            vars = vars
        })

        if not ok then
            return
        end

        -- Close channel
        env_channel:close()
    end)

    -- Set initial response status
    res:set_status(http.STATUS.OK)

    -- Consumer coroutine (main thread)
    while true do
        -- Receive data from channel
        local data, ok = env_channel:receive()

        if not ok then
            -- Channel closed or error, exit
            break
        end

        -- Write JSON response chunk
        local packed, encode_err = json.encode(data)
        if encode_err then
            res:set_status(http.STATUS.INTERNAL_ERROR)
            res:write_json({ error = "JSON encoding failed: " .. encode_err })
            break
        end

        res:write(packed .. "\n")
        res:flush()
    end

    -- Ensure the response is properly terminated
    res:flush()
end

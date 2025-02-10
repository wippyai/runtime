local json = require("json")
local http = require("httpctx")
local env = require("env")

function envdump()
    -- Get request context
    local res = http.response()

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
        env_channel:send({
            type = "important",
            vars = important
        })

        -- Send all variables
        env_channel:send({
            type = "all",
            vars = vars
        })

        -- Close channel
        env_channel:close()
    end)

    -- Consumer coroutine (main thread)
    while true do
        -- Receive data from channel
        local data, ok = env_channel:receive()

        if not ok then
            -- Channel closed, exit
            break
        end

        -- Write JSON response
        local packed = json.encode(data)
        res:write(packed .. "\n")
        res:flush()
    end
end
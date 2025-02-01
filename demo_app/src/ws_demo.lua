local time = require("time")
local json = require("json")
local httpctx = require("httpctx")
local websocket = require("websocket")

function websocket_demo()
    -- Get request context
    local res = httpctx.response()

    -- Create channel for communication
    local ws_channel = channel.new(1)

    -- Spawn WebSocket client coroutine
    coroutine.spawn(function()
        -- Connect to WebSocket server
        local client, err = websocket.connect("wss://echo.websocket.org", {
            headers = { ["User-Agent"] = "Lua WebSocket Demo" },
            dial_timeout = "5s",
            read_timeout = "30s",
            write_timeout = "10s"
        })

        if not client then
            ws_channel:send({ error = "Failed to connect: " .. err })
            ws_channel:close()
            return
        end

        -- Start receiver coroutine for WebSocket messages
        coroutine.spawn(function()
            local ch = client:receive()
            while true do
                local msg, ok = ch:receive()
                if not ok then
                    ws_channel:send({ status = "closed", message = "Connection closed" })
                    break
                end

                if msg.type == websocket.TYPE_TEXT then
                    -- Forward received message to main channel
                    ws_channel:send({ status = "received", data = msg.data })
                elseif msg.type == websocket.TYPE_CLOSE then
                    ws_channel:send({ status = "closed", code = msg.code, reason = msg.reason })
                    break
                end
            end
        end)

        -- Ticker logic
        local start_time = time.now()
        local ticks = 0
        local max_ticks = 10 -- Reduced for demo

        while ticks < max_ticks do
            -- Create tick data
            local elapsed = time.now():sub(start_time)
            local data = {
                tick = ticks,
                elapsed = tostring(elapsed)
            }

            -- Send data over WebSocket
            local ok, err = client:send(json.encode(data))
            if not ok then
                ws_channel:send({ error = "Failed to send: " .. err })
                break
            end

            time.sleep(time.parse_duration("1s"))
            ticks = ticks + 1
        end

        -- Clean up
        client:close(websocket.CLOSE_CODES.NORMAL, "Demo completed")
        ws_channel:close()
    end)

    -- Consumer coroutine (main thread)
    while true do
        -- Receive data from channel
        local data, ok = ws_channel:receive()

        if not ok then
            -- Channel closed, exit
            break
        end

        -- Write JSON response
        local packed, err = json.encode(data)
        if packed then
            res:write(packed .. "\n")
            res:flush()
        end
    end
end

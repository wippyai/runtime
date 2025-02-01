local time = require("time")
local json = require("json")
local httpctx = require("httpctx")
local logger = require("logger")
local websocket = require("websocket")

function websocket_demo()
    local log = logger:named("websocket_demo")
    local res = httpctx.response()
    log:info("Starting WebSocket demo handler")

    local ws_channel = channel.new(1)

    coroutine.spawn(function()
        log:info("Attempting to connect to WebSocket server", {url = "wss://echo.websocket.org"})

        local client, err = websocket.connect("wss://echo.websocket.org", {
            headers = { ["User-Agent"] = "Lua WebSocket Demo" },
            dial_timeout = "5s",
            read_timeout = "30s",
            write_timeout = "10s"
        })

        if not client then
            log:error("WebSocket connection failed", {error = err})
            ws_channel:send({ error = "Failed to connect: " .. err })
            ws_channel:close()
            return
        end

        log:info("WebSocket connection established")

        -- Start receiver for echo responses
        coroutine.spawn(function()
            local ch = client:receive()
            while true do
                local msg, ok = ch:receive()
                if not ok then
                    log:warn("WebSocket receive channel closed")
                    ws_channel:send({ status = "closed", message = "Connection closed" })
                    break
                end

                if msg.type == websocket.TYPE_TEXT then
                    log:info("Received echo response", {data = msg.data})
                    ws_channel:send({ status = "received", data = msg.data })
                elseif msg.type == websocket.TYPE_CLOSE then
                    log:info("Received WebSocket close frame", {code = msg.code, reason = msg.reason})
                    ws_channel:send({ status = "closed", code = msg.code, reason = msg.reason })
                    break
                end
            end
        end)

        -- Send test messages
        local messages = {
            "Hello from Lua!",
            "This is a test message",
            "Testing WebSocket send",
        }

        for i, msg in ipairs(messages) do
            log:info("Sending test message", {message = msg})
            local ok, err = client:send(msg)
            if not ok then
                log:error("Failed to send message", {error = err})
                break
            end
            time.sleep(time.parse_duration("1s"))
        end

        log:info("Closing WebSocket connection")
        client:close(websocket.CLOSE_CODES.NORMAL, "Test completed")
        ws_channel:close()
    end)

    -- Consumer loop for responses
    while true do
        local data, ok = ws_channel:receive()
        if not ok then
            log:info("Channel closed, exiting consumer loop")
            break
        end

        local packed, err = json.encode(data)
        if packed then
            res:write(packed .. "\n")
            res:flush()
        else
            log:error("Failed to encode JSON response", {error = err})
        end
    end

    log:info("WebSocket demo handler completed")
end
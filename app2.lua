---@diagnostic disable: undefined-global, undefined-field
function App()
    -- Create named channel for messages
    local message_channel = channel.named("messages")

    -- Spawn a coroutine that only yields messages
    coroutine.spawn(function()
        while true do
            -- Sleep for a bit
            time.sleep(time.parse_duration("1s"))

            -- Yield a message
            yield_message({
                type = "coroutine_message",
                content = "Message from coroutine",
                timestamp = time.now():format("15:04:05")
            })
        end
    end)

    -- Main loop to receive messages from the named channel
    while true do
        local msg, ok = message_channel:receive()
        if not ok then
            print("Channel closed")
            break
        end

        print("Received message: " .. json.encode(msg))

        -- Yield both a message and a view for the received message
        --yield_message({
        --    type = "channel_received",
        --    content = result.value,
        --    timestamp = time.now():format("15:04:05")
        --})
        yield_view("application state")
    end
end

return App

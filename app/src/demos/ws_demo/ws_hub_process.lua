local time = require("time")

-- Main hub process that will:
-- 1. Accept WebSocket join/leave messages
-- 2. Maintain a list of connected clients
-- 3. Broadcast a message to all clients every second

local function run()
    -- Initialize state
    local state = {
        connected_clients = {},
        client_count = 0,
        message_count = 0
    }

    -- Register this process with a name for easy discovery
    process.registry.register("ws_hub")

    print("WebSocket Hub started with PID:", process.pid())

    -- Create channels for events
    local inbox = process.inbox()    -- For getting messages from the relay
    local ticker = time.ticker("1s") -- For sending periodic updates
    local events = process.events()  -- For process lifecycle events

    -- Set process options
    process.set_options({
        trap_links = true -- We want to handle link downs
    })

    -- Main event loop
    local running = true
    while running do
        local result = channel.select({
            inbox:case_receive(),            -- Messages from clients
            ticker:channel():case_receive(), -- Ticker for periodic broadcast
            events:case_receive()            -- System events
        })

        if result.channel == inbox then
            -- Process incoming message from relay
            local msg = result.value
            local topic = msg:topic()
            local payload = msg:payload():data()

            if topic == "ws.join" then
                -- New client connected
                local client_pid = payload
                print("WebSocket client joined:", client_pid)

                -- Add client to our list
                state.connected_clients[client_pid] = true
                state.client_count = state.client_count + 1

                -- Send welcome message to this client
                print(process.send(client_pid, "ws.message", {
                    type = "welcome",
                    message = "Welcome to the WebSocket Hub!",
                    clients = state.client_count
                }))
            elseif topic == "ws.leave" then
                -- Client disconnected
                local client_pid = payload
                print("WebSocket client left:", client_pid)

                -- Remove client from our list
                if state.connected_clients[client_pid] then
                    state.connected_clients[client_pid] = nil
                    state.client_count = state.client_count - 1
                end
            elseif topic == "ws.message" then
                -- Message from client, echo it back to all clients
                print("Received message from client:", payload)

                -- Broadcast to all clients
                for client_pid, _ in pairs(state.connected_clients) do
                    process.send(client_pid, "ws.message", {
                        type = "echo",
                        from = msg:from() and msg:from():to_string() or "unknown",
                        message = payload,
                        clients = state.client_count
                    })
                end
            end
        elseif result.channel == ticker:channel() then
            -- Time to send a periodic update to all clients
            state.message_count = state.message_count + 1

            -- Skip if no clients connected
            if state.client_count == 0 then
                goto continue
            end

            -- Create message with current time and stats
            local update = {
                type = "update",
                time = time.now():format_rfc3339(),
                sequence = state.message_count,
                clients = state.client_count
            }

            -- Broadcast to all clients
            print(string.format("Broadcasting update #%d to %d clients",
                state.message_count, state.client_count))

            for client_pid, _ in pairs(state.connected_clients) do
            print(client_pid)
                process.send(client_pid, "ws.message", update)
            end
        elseif result.channel == events then
            -- Process a system event
            local event = result.value

            if event.kind == process.event.CANCEL then
                print("WebSocket Hub received cancel request")
                running = false
            elseif event.kind == process.event.LINK_DOWN then
                print("Link down event from:", event.event.from)
            end
        end

        ::continue::
    end

    -- Cleanup before exit
    ticker:stop()

    print("WebSocket Hub shutting down")
    return { status = "shutdown", clients = state.client_count }
end

return { run = run }

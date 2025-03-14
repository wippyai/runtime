local time = require("time")

-- Main hub process that will:
-- 1. Accept WebSocket join/leave messages
-- 2. Maintain a list of connected clients
-- 3. Broadcast a message to all clients every second

local function run()
    -- Initialize state
    local state = {
        connected_clients = {}, -- Map of client_pid -> true
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

    -- Helper function to safely extract sender information
    local function get_sender_info(msg)
        local sender = "unknown"

        -- Safely check if from method exists and returns a value
        if msg and type(msg.from) == "function" then
            local from_result = msg:from()
            if from_result then
                sender = from_result
            end
        end

        return sender
    end

    -- Helper function to extract client_pid from payload
    local function extract_client_pid(payload)
        -- Handle different payload types
        if type(payload) == "string" then
            return payload -- Direct string PID
        elseif type(payload) == "table" then
            -- Check for structured payload
            if payload.client_pid then
                return payload.client_pid
            elseif payload.ws_pid then
                return payload.ws_pid
            end
        end

        -- If we couldn't extract a valid client_pid, return nil
        return nil
    end

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
            if not msg then
                goto continue
            end

            local topic = msg:topic()
            if not topic then
                goto continue
            end

            -- Safely extract payload
            local payload = nil
            if msg:payload() then
                payload = msg:payload():data()
            end

            -- If payload is nil, skip processing
            if payload == nil then
                print("Warning: Received nil payload for topic:", topic)
                goto continue
            end

            local sender = get_sender_info(msg)

            if topic == "ws.join" then
                -- Extract client_pid from the payload (could be string or table)
                local client_pid = extract_client_pid(payload)

                if not client_pid then
                    print("Warning: Could not extract valid client PID from join payload:",
                          type(payload) == "table" and "table" or tostring(payload))
                    goto continue
                end

                -- Ensure client_pid is a string
                client_pid = tostring(client_pid)

                print("WebSocket client joined:", client_pid)

                -- Add client to our list with a valid key
                state.connected_clients[client_pid] = true
                state.client_count = state.client_count + 1

                -- Send welcome message to this client
                local ok, err = process.send(client_pid, "ws.message", {
                    type = "welcome",
                    message = "Welcome to the WebSocket Hub!",
                    clients = state.client_count
                })

                if not ok then
                    print("Error sending welcome message:", err)
                end

            elseif topic == "ws.leave" then
                -- Extract client_pid from the payload (could be string or table)
                local client_pid = extract_client_pid(payload)

                if not client_pid then
                    print("Warning: Could not extract valid client PID from leave payload:",
                          type(payload) == "table" and "table" or tostring(payload))
                    goto continue
                end

                -- Ensure client_pid is a string
                client_pid = tostring(client_pid)

                print("WebSocket client left:", client_pid)

                -- Remove client from our list if it exists
                if state.connected_clients[client_pid] then
                    state.connected_clients[client_pid] = nil
                    state.client_count = state.client_count - 1
                end

            elseif topic == "ws.heartbeat" then
                -- Process heartbeat from relay
                if type(payload) == "table" then
                    print("Received heartbeat from client:",
                        payload.ws_pid or "unknown",
                        "uptime:", payload.uptime or "unknown",
                        "messages:", payload.message_count or 0)
                else
                    print("Received heartbeat from client:", tostring(payload))
                end

            elseif topic == "ws.message" then
                -- Message from client, echo it back to all clients
                print("Received message from client:",
                      type(payload) == "table" and "table" or tostring(payload))

                -- Broadcast to all clients
                for client_pid, _ in pairs(state.connected_clients) do
                    if client_pid then
                        process.send(client_pid, "ws.message", {
                            type = "echo",
                            from = sender,
                            message = payload,
                            clients = state.client_count
                        })
                    end
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
                if client_pid then
                    process.send(client_pid, "ws.message", update)
                end
            end

        elseif result.channel == events then
            -- Process a system event
            local event = result.value

            if event.kind == process.event.CANCEL then
                print("WebSocket Hub received cancel request")
                running = false
            elseif event.kind == process.event.LINK_DOWN then
                print("Link down event from:", event.event and event.event.from or "unknown")
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
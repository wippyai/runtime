local time = require("time")
local json = require("json")
local actor = require("actor")
local funcs = require("funcs")

-- Constants
local PING_INTERVAL = "60s"   -- Send stats to central hub every 5 seconds
local UPDATE_INTERVAL = "30s" -- Send updates to clients every 1 second
local USER_HUB_REGISTRY_PREFIX = "user_hub."
local WS_JOIN_TOPIC = "ws.join"
local WS_LEAVE_TOPIC = "ws.leave"
local WS_MESSAGE_TOPIC = "ws.message"
local WS_CANCEL_TOPIC = "ws.cancel" -- Topic to notify clients of cancellation
local WELCOME_TOPIC = "welcome"
local UPDATE_TOPIC = "update"
local STATS_PING_TOPIC = "stats.ping" -- Topic for sending stats to central hub

-- User Hub Process - Handles WebSocket connections for a specific user
local function run(args)
    -- Verify required arguments
    if not args or not args.user_id then
        print("Error: user_id is required")
        return { error = "Missing required arguments" }
    end

    local user_id = args.user_id
    local user_metadata = args.user_metadata or {}
    local central_hub_pid = args.central_hub_pid

    -- Initialize actor state
    local initial_state = {
        user_id = user_id,
        metadata = user_metadata,
        connected_clients = {}, -- Map of client_pid -> { last_activity = timestamp }
        client_count = 0,
        last_activity = time.now(),
        start_time = time.now(), -- Store start time for uptime calculation
        messages_handled = 0,
        ping_ticker = nil,
        update_ticker = nil,
        central_hub_pid = central_hub_pid
    }

    -- Define handlers for different message topics
    local handlers = {
        -- Initialize the actor
        __init = function(state)
            -- Register this process with the user's ID for easy discovery
            local registry_name = USER_HUB_REGISTRY_PREFIX .. state.user_id
            process.registry.register(registry_name)
            print("User Hub started for user:", state.user_id, "with PID:", process.pid())

            -- Set process options
            process.set_options({
                trap_links = true
            })

            -- Create ping ticker to send stats to central hub
            state.ping_ticker = time.ticker(PING_INTERVAL)
            state.register_channel(state.ping_ticker:channel(), function(s, _, ok)
                if ok then
                    if s.central_hub_pid then
                        -- Send stats to central hub
                        process.send(s.central_hub_pid, STATS_PING_TOPIC, {
                            user_id = s.user_id,
                            client_count = s.client_count,
                            last_activity = s.last_activity:format_rfc3339(),
                            messages_handled = s.messages_handled
                        })
                    end
                end
                return s
            end)

            -- Create update ticker to send updates to clients
            state.update_ticker = time.ticker(UPDATE_INTERVAL)
            state.register_channel(state.update_ticker:channel(), function(s, _, ok)
                if ok then
                    -- Send update to all connected clients
                    broadcast_to_clients(s, UPDATE_TOPIC, {
                        user_id = s.user_id,
                        time = time.now():format_rfc3339(),
                        message = "Regular update"
                    })
                end
                return s
            end)

            return state
        end,

        -- Handle system events
        __on_event = function(state, event)
            -- handle child processes
            return state
        end,

        -- Handle cancellation
        __on_cancel = function(state)
            print("User Hub for", state.user_id, "received cancel request")

            -- Send cancellation notification to all connected clients
            broadcast_to_clients(state, WS_CANCEL_TOPIC, {
                user_id = state.user_id,
                time = time.now():format_rfc3339(),
                reason = "Hub shutting down"
            })

            -- Stop tickers
            if state.ping_ticker then
                state.ping_ticker:stop()
            end

            if state.update_ticker then
                state.update_ticker:stop()
            end

            return actor.exit({ status = "shutdown", user_id = state.user_id })
        end,

        -- Handle WebSocket join
        [WS_JOIN_TOPIC] = function(state, payload)
            local client_pid = payload.client_pid

            print("Client joined user hub for", state.user_id, ":", client_pid)

            -- Add client to our list
            state.connected_clients[client_pid] = { connected_on = time.now() }
            state.client_count = state.client_count + 1
            state.last_activity = time.now()

            -- Send welcome message with user ID
            process.send(client_pid, WELCOME_TOPIC, {
                user_id = state.user_id,
                time = time.now():format_rfc3339()
            })

            return state
        end,

        -- Handle WebSocket leave
        [WS_LEAVE_TOPIC] = function(state, payload)
            -- Get sender PID from the message context
            local client_pid = payload.client_pid

            if state.connected_clients[client_pid] then
                print("Client left user hub for", state.user_id, ":", client_pid)
                state.connected_clients[client_pid] = nil
                state.client_count = state.client_count - 1
                state.last_activity = time.now()
            end

            return state
        end,

        -- Handle WebSocket messages
        [WS_MESSAGE_TOPIC] = function(state, payload)
            local message, err = json.decode(payload)
            if not message then
                print("Error decoding message from client", state.user_id, ":", err)
                return state
            end

            -- Update client's last activity time
            state.last_activity = time.now()
            state.messages_handled = state.messages_handled + 1

            -- Echo message back to all clients with user_id attached
            local response = {
                user_id = state.user_id,
                time = time.now():format_rfc3339(),
                message = message
            }


            -- Broadcast to all clients (todo: make it accept command)
            broadcast_to_clients(state, UPDATE_TOPIC, response)

            return state
        end,

        __default = function(state, topic, payload)
            print("Unhandled message in user hub for", state.user_id, ":", topic, json.encode(payload))

            -- relay to all clients
            broadcast_to_clients(state, topic, payload)

            --unt":0,"metadata":{"auth_time":1742066279,"user_id":"wolfy-j","user_metadata":{"sf_instance_token":"asd"}},"uptime":"29.009048977s"}    {"pid": "{Antares@app:processes|app.users.relay:user_hub|0x00002}"}
            --2025-03-15 15:18:29     INFO    hosts   Unhandled message in user hub for wolfy-j : ws.heartbeat {"client_pid":"{Antares@app:gateway|ws:conn|0x00001}","message_count":0,"metadata":{"auth_time":1742066279,"user_id":"wolfy-j","user_metadata":{"sf_instance_token":"asd"}},"uptime":"30.000431237s"}    {"pid": "{Antares@app:processes|app.users.relay:user_hub|0x00002}"}
            --2025-03-15 15:18:29     INFO    hosts   Received stats from hub for user wolfy-j : clients: 1 messages: 0       {"pid": "{Antares@app:processes|app.users.relay:central_hub|0x00001}"}
            --2025-03-15 15:18:29     INFO    hosts   Received message from client  for user wolfy-j : "{\"type\":\"ping\"}"  {"pid": "{Antares@app:processes|app.users.relay:user_hub|0x00002}"}
            --2025-03-15 15:18:30     INFO    hosts   Unhandled message in user hub for wolfy-j : ws.heartbeat {"client_pid":"{Antares@app:gateway|ws:conn|0x00001}","message_count":1,"metadata":{"auth_time":1742066279,"user_id":"wolfy-j","user_metadata":{"sf_instance_token":"asd"}},"uptime":"31.000816944s"}    {"pid": "{Antares@app:processes|app.users.relay:user_hub|0x00002}"}
            --2025-03-15 15:18:31     INFO    hosts   Unhandled message in user hub for wolfy-j : ws.heartbeat {"client_pid":"{Antares@app:gateway|ws:conn|0x00001}","message_count":1,"metadata":{"auth_time":1742066279,"user_id":"wolfy-j","user_metadata":{"sf_instance_token":"asd"}},"uptime":"32.000467682s"}    {"pid": "{Antares@app:processes|app.users.relay:user_hub|0x00002}
            return state
        end
    }

    -- Helper function to broadcast a message to all connected clients
    function broadcast_to_clients(state, topic, message)
        for client_pid, _ in pairs(state.connected_clients) do
            process.send(client_pid, topic, message)
        end
    end

    -- Create and run the actor
    local user_hub_actor = actor.new(initial_state, handlers)
    return user_hub_actor.run()
end

return { run = run }

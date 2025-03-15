local time = require("time")
local json = require("json")
local actor = require("actor")

-- Constants
local USER_INACTIVITY_TIMEOUT = "60s"  -- 1 minute inactivity timeout
local UPDATE_INTERVAL = "1s"           -- Send update every second
local CHECK_EXIT_INTERVAL = "10s"      -- Check exit conditions every 10 seconds
local CENTRAL_HUB_REGISTRY_NAME = "central_hub"
local USER_HUB_REGISTRY_PREFIX = "user_hub."
local WS_JOIN_TOPIC = "ws.join"
local WS_LEAVE_TOPIC = "ws.leave"
local WS_MESSAGE_TOPIC = "ws.message"
local PING_TOPIC = "ping"
local PONG_TOPIC = "pong"
local UPDATE_TOPIC = "update"
local WELCOME_TOPIC = "welcome"
local USER_ACTIVITY_TOPIC = "user.activity"
local USER_HUB_EXIT_TOPIC = "user.hub.exit"
local HUB_SHUTDOWN_TOPIC = "hub.shutdown"

-- User Hub Process - Handles WebSocket connections for a specific user
local function run(args)
    -- Verify required arguments
    if not args or not args.user_id then
        print("Error: user_id is required")
        return { error = "Missing required arguments" }
    end

    local user_id = args.user_id
    local user_metadata = args.user_metadata or {}
    local inactivity_timeout = args.inactivity_timeout or USER_INACTIVITY_TIMEOUT

    -- Initialize actor state
    local initial_state = {
        user_id = user_id,
        metadata = user_metadata,
        connected_clients = {},  -- Map of client_pid -> { last_activity = timestamp }
        client_count = 0,
        last_activity = time.now(),
        update_sequence = 0,
        inactivity_timeout = time.parse_duration(inactivity_timeout),
        central_hub_pid = nil,
        update_ticker = nil,
        check_exit_ticker = nil,
        should_exit = false
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

            -- Find the central hub
            state.central_hub_pid = process.registry.lookup(CENTRAL_HUB_REGISTRY_NAME)

            -- Create update ticker
            state.update_ticker = time.ticker(UPDATE_INTERVAL)
            state.register_channel(state.update_ticker:channel(), function(s, _, ok)
                if ok then
                    send_update_to_clients(s)
                end
                return s
            end)

            -- Create exit check ticker
            state.check_exit_ticker = time.ticker(CHECK_EXIT_INTERVAL)
            state.register_channel(state.check_exit_ticker:channel(), function(s, _, ok)
                if ok then
                    if check_exit_conditions(s) then
                        s.should_exit = true
                        return actor.exit(create_exit_result(s))
                    end
                end
                return s
            end)

            return state
        end,

        -- Handle system events
        __on_event = function(state, event)
            if event.kind == process.event.LINK_DOWN then
                -- A linked client might have disconnected unexpectedly
                local down_pid = event.event and event.event.from
                if down_pid and state.connected_clients[down_pid] then
                    print("Client connection lost for user", state.user_id, ":", down_pid)
                    state.connected_clients[down_pid] = nil
                    state.client_count = state.client_count - 1

                    -- Update activity time on client disconnection
                    update_activity(state)
                end
            end

            return state
        end,

        -- Handle cancellation
        __on_cancel = function(state)
            print("User Hub for", state.user_id, "received cancel request")
            return clean_exit(state)
        end,

        -- Handle WebSocket join
        [WS_JOIN_TOPIC] = function(state, payload)
            -- Get sender PID from the message context
            local client_pid = tostring(coroutine.yield())

            print("Client joined user hub for", state.user_id, ":", client_pid)

            -- Add client to our list
            state.connected_clients[client_pid] = {
                last_activity = time.now()
            }
            state.client_count = state.client_count + 1

            -- Send welcome message
            process.send(client_pid, WELCOME_TOPIC, {
                message = "Welcome to your personal hub, " .. state.user_id,
                user_id = state.user_id,
                clients = state.client_count,
                time = time.now():format_rfc3339()
            })

            -- Update activity time
            update_activity(state)

            return state
        end,

        -- Handle WebSocket leave
        [WS_LEAVE_TOPIC] = function(state, payload)
            -- Get sender PID from the message context
            local client_pid = tostring(coroutine.yield())

            if state.connected_clients[client_pid] then
                print("Client left user hub for", state.user_id, ":", client_pid)
                state.connected_clients[client_pid] = nil
                state.client_count = state.client_count - 1

                -- Update activity time on client disconnection
                update_activity(state)
            end

            return state
        end,

        -- Handle WebSocket messages
        [WS_MESSAGE_TOPIC] = function(state, payload)
            -- Get sender PID from the message context
            local client_pid = tostring(coroutine.yield())

            -- Update client's last activity time
            if state.connected_clients[client_pid] then
                state.connected_clients[client_pid].last_activity = time.now()
            end

            -- Update hub's activity time
            update_activity(state)

            return state
        end,

        -- Handle ping messages
        [PING_TOPIC] = function(state, payload)
            -- Get sender PID from the message context
            local client_pid = tostring(coroutine.yield())

            -- Update client's last activity time if it's a client
            if state.connected_clients[client_pid] then
                state.connected_clients[client_pid].last_activity = time.now()
            end

            -- Update hub's activity time
            update_activity(state)

            -- Send pong response
            process.send(client_pid, PONG_TOPIC, {
                time = time.now():format_rfc3339()
            })

            return state
        end
    }

    -- Helper function to update activity time
    function update_activity(state)
        state.last_activity = time.now()

        -- Notify central hub of activity
        if state.central_hub_pid then
            process.send(state.central_hub_pid, USER_ACTIVITY_TOPIC, {
                user_id = state.user_id,
                clients = state.client_count
            })
        end
    end

    -- Check if we should exit due to inactivity
    function check_exit_conditions(state)
        -- Calculate inactivity duration
        local inactivity_duration = time.now():sub(state.last_activity)

        -- If no clients are connected and we've been inactive for too long, exit
        if state.client_count == 0 and inactivity_duration:seconds() > state.inactivity_timeout:seconds() then
            print("User Hub for", state.user_id, "shutting down due to inactivity")

            -- Notify central hub that we're exiting
            if state.central_hub_pid then
                process.send(state.central_hub_pid, USER_HUB_EXIT_TOPIC, {
                    user_id = state.user_id
                })
            end

            return true
        end

        return false
    end

    -- Send update to all connected clients
    function send_update_to_clients(state)
        if state.client_count == 0 then
            return
        end

        state.update_sequence = state.update_sequence + 1

        -- Create update message
        local update = {
            time = time.now():format_rfc3339(),
            sequence = state.update_sequence,
            user_id = state.user_id,
            connected_clients = state.client_count
        }

        -- Only log occasionally to reduce spam
        if state.update_sequence % 10 == 0 then
            print("Sending update #", state.update_sequence, "to", state.client_count, "clients for user:", state.user_id)
        end

        -- Send update to all clients
        for client_pid, _ in pairs(state.connected_clients) do
            if client_pid then
                process.send(client_pid, UPDATE_TOPIC, update)
            end
        end
    end

    -- Clean up and prepare exit
    function clean_exit(state)
        -- Stop tickers
        if state.update_ticker then
            state.update_ticker:stop()
        end

        if state.check_exit_ticker then
            state.check_exit_ticker:stop()
        end

        -- Notify all clients that we're shutting down
        for client_pid, _ in pairs(state.connected_clients) do
            process.send(client_pid, HUB_SHUTDOWN_TOPIC, {
                message = "User hub is shutting down",
                user_id = state.user_id
            })
        end

        return actor.exit(create_exit_result(state))
    end

    -- Create exit result structure
    function create_exit_result(state)
        return {
            status = "shutdown",
            user_id = state.user_id,
            clients = state.client_count
        }
    end

    -- Create and run the actor
    local user_hub_actor = actor.new(initial_state, handlers)
    return user_hub_actor.run()
end

return { run = run }
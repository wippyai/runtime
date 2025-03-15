local time = require("time")
local json = require("json")
local actor = require("actor")

-- Constants
local USER_INACTIVITY_TIMEOUT = "30s" -- 30 seconds inactivity timeout
local CHECK_EXIT_INTERVAL = "10s"     -- Check exit conditions every 10 seconds
local USER_HUB_REGISTRY_PREFIX = "user_hub."
local WS_JOIN_TOPIC = "ws.join"
local WS_LEAVE_TOPIC = "ws.leave"
local WS_MESSAGE_TOPIC = "ws.message"
local WELCOME_TOPIC = "welcome"
local UPDATE_TOPIC = "update"

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
        connected_clients = {}, -- Map of client_pid -> { last_activity = timestamp }
        client_count = 0,
        last_activity = time.now(),
        inactivity_timeout = time.parse_duration(inactivity_timeout),
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

            ---- Create exit check ticker
            --state.check_exit_ticker = time.ticker(CHECK_EXIT_INTERVAL)
            --state.register_channel(state.check_exit_ticker:channel(), function(s, _, ok)
            --    if ok then
            --        if check_exit_conditions(s) then
            --            s.should_exit = true
            --            return actor.exit({ status = "shutdown", user_id = s.user_id })
            --        end
            --    end
            --    return s
            --end)

            return state
        end,

        -- Handle system events
        __on_event = function(state, event)
            if event.kind == process.event.LINK_DOWN then
                -- A linked client might have disconnected unexpectedly
                local down_pid = event.from
                if down_pid and state.connected_clients[down_pid] then
                    print("Client connection lost for user", state.user_id, ":", down_pid)
                    state.connected_clients[down_pid] = nil
                    state.client_count = state.client_count - 1
                    state.last_activity = time.now()
                end
            end

            return state
        end,

        -- Handle cancellation
        __on_cancel = function(state)
            print("User Hub for", state.user_id, "received cancel request")
            if state.check_exit_ticker then
                state.check_exit_ticker:stop()
            end
            return actor.exit({ status = "shutdown", user_id = state.user_id })
        end,

        -- Handle WebSocket join
        [WS_JOIN_TOPIC] = function(state, payload)
            local client_pid = payload.client_pid

            print("Client joined user hub for", state.user_id, ":", client_pid)

            -- Add client to our list
            state.connected_clients[client_pid] = {
                last_activity = time.now()
            }
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
            local client_pid = tostring(coroutine.yield())

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
            -- Get sender PID from the message context
            local client_pid = tostring(coroutine.yield())

            -- Update client's last activity time
            if state.connected_clients[client_pid] then
                state.connected_clients[client_pid].last_activity = time.now()
            end
            state.last_activity = time.now()

            -- Echo message back to client with user_id attached
            if payload then
                local response = payload

                -- Add user_id to response if it's a table
                if type(payload) == "table" then
                    response.user_id = state.user_id
                end

                -- Send response back to the client
                process.send(client_pid, UPDATE_TOPIC, response)
            end

            return state
        end
    }

    -- Check if we should exit due to inactivity
    function check_exit_conditions(state)
        -- Calculate inactivity duration
        local inactivity_duration = time.now():sub(state.last_activity)

        -- If no clients are connected and we've been inactive for too long, exit
        if state.client_count == 0 and inactivity_duration:seconds() > state.inactivity_timeout:seconds() then
            print("User Hub for", state.user_id, "shutting down due to inactivity")
            return true
        end

        return false
    end

    -- Create and run the actor
    local user_hub_actor = actor.new(initial_state, handlers)
    return user_hub_actor.run()
end

return { run = run }

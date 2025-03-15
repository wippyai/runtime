local time = require("time")
local json = require("json")
local actor = require("actor")

-- Constants
local CENTRAL_HUB_REGISTRY_NAME = "central_hub"
local WS_JOIN_TOPIC = "ws.join"
local WS_CONTROL_TOPIC = "ws.control"
local STATS_PING_TOPIC = "stats.ping"
local USER_HUB_PROCESS_ID = "app.users.relay:user_hub"
local USER_HUB_HOST = "app:processes"
local USER_HUB_INACTIVITY_TIMEOUT = "300s"
local GC_CHECK_INTERVAL = "120s" -- Check for inactive hubs every 30 seconds

-- Central Hub Process - Central hub that manages user-specific hubs
local function run()
    -- Define the initial state
    local initial_state = {
        user_hubs = {}, -- Map of user_id -> { hub_pid, last_ping, client_count, messages_handled }
        total_hubs = 0,
        gc_ticker = nil -- Garbage collection ticker
    }

    -- Define handlers for different message topics
    local handlers = {
        -- Initialize the actor
        __init = function(state)
            -- Register this process with a name for easy discovery
            process.registry.register(CENTRAL_HUB_REGISTRY_NAME)
            print("Central Hub started with PID:", process.pid())

            -- Create garbage collection ticker
            state.gc_ticker = time.ticker(GC_CHECK_INTERVAL)
            state.register_channel(state.gc_ticker:channel(), function(s, _, ok)
                if ok then
                    -- Check for inactive user hubs
                    check_inactive_hubs(s)
                end
                return s
            end)

            return state
        end,

        -- Handle system events
        __on_event = function(state, event)
            if event.kind == process.event.EXIT then
                -- A monitored process has exited
                local from_pid = event.from

                print("EVNT FROM PID", from_pid, "EXITED", event.reason)

                if from_pid then
                    -- Find which user hub this was
                    for user_id, hub_info in pairs(state.user_hubs) do
                        if hub_info.hub_pid == from_pid then
                            print("User hub for", user_id, "has exited")
                            state.user_hubs[user_id] = nil
                            state.total_hubs = state.total_hubs - 1
                            break
                        end
                    end
                end
            end

            return state
        end,

        -- Handle cancellation
        __on_cancel = function(state)
            print("Central Hub received cancel request")

            for user_id, hub_info in pairs(state.user_hubs) do
                process.cancel(hub_info.hub_pid)
            end

            -- Stop GC ticker
            if state.gc_ticker then
                state.gc_ticker:stop()
            end

            print("Central Hub shutting down")
            return actor.exit({ status = "shutdown", hubs = state.total_hubs })
        end,

        -- Handle WebSocket join requests
        [WS_JOIN_TOPIC] = function(state, payload)
            handle_client_connection(state, payload.client_pid, payload.metadata)
            return state
        end,

        -- Handle stats ping from user hubs
        [STATS_PING_TOPIC] = function(state, payload)
            local user_id = payload.user_id

            if user_id and state.user_hubs[user_id] then
                -- Update hub stats
                state.user_hubs[user_id].client_count = payload.client_count
                state.user_hubs[user_id].messages_handled = payload.messages_handled

                if payload.last_activity then
                    local activity_time, err = time.parse(time.RFC3339, payload.last_activity)
                    if activity_time then
                        state.user_hubs[user_id].last_activity = activity_time
                    end
                end
            end

            return state
        end
    }

    -- Helper function to extract user_id from metadata
    local function extract_user_id(metadata)
        if type(metadata) ~= "table" then
            return nil
        end
        return metadata.user_id
    end

    -- Function to handle client connection
    function handle_client_connection(state, client_pid, metadata)
        -- Extract user ID from metadata
        local user_id = extract_user_id(metadata)
        if not user_id then
            print("Missing user_id in metadata, cannot route client:", client_pid)
            return
        end

        print("Handling connection for user:", user_id, "client:", client_pid)

        -- Get or create user hub for this user
        local user_hub_pid = create_user_hub(state, user_id, metadata.user_metadata)
        if not user_hub_pid then
            print("Failed to get or create user hub for", user_id)
            return
        end

        -- Send redirection control message to WebSocket relay
        print("Redirecting client", client_pid, "to user hub", user_hub_pid)
        process.send(client_pid, WS_CONTROL_TOPIC, {
            target_pid = user_hub_pid,
            metadata = metadata
        })

        -- Update stats
        if state.user_hubs[user_id] then
            state.user_hubs[user_id].last_ping = time.now()
        end
    end

    -- Function to create a new user hub for a specific user
    function create_user_hub(state, user_id, user_metadata)
        -- Check if a hub already exists for this user
        if state.user_hubs[user_id] and state.user_hubs[user_id].hub_pid then
            return state.user_hubs[user_id].hub_pid
        end

        -- Create a new user hub for this user
        print("Creating new user hub for user:", user_id)

        -- Spawn a monitored user hub process
        local hub_pid, err = process.spawn_monitored(
            USER_HUB_PROCESS_ID,
            USER_HUB_HOST,
            {
                user_id = user_id,
                user_metadata = user_metadata,
                inactivity_timeout = USER_HUB_INACTIVITY_TIMEOUT,
                central_hub_pid = process.pid() -- Pass central hub PID to user hub
            }
        )

        if not hub_pid then
            print("Failed to create user hub for", user_id, ":", err)
            return nil
        end

        -- Store the hub information
        state.user_hubs[user_id] = {
            hub_pid = hub_pid,
            created_at = time.now(),
            last_activity = time.now(),
            client_count = 0,
            messages_handled = 0,
        }

        state.total_hubs = state.total_hubs + 1
        print("Created user hub for", user_id, "with PID:", hub_pid)

        return hub_pid
    end

    -- Function to check for inactive user hubs
    function check_inactive_hubs(state)
        local now = time.now()
        local inactivity_duration = time.parse_duration(USER_HUB_INACTIVITY_TIMEOUT)

        function check_inactive_hubs(state)
            local now = time.now()
            local inactivity_duration = time.parse_duration(USER_HUB_INACTIVITY_TIMEOUT)

            for user_id, hub_info in pairs(state.user_hubs) do
                -- Skip hubs that are already being terminated
                if hub_info.terminating then
                    goto continue
                end

                -- Check if hub has been inactive for too long
                local last_activity = now:sub(hub_info.last_activity)

                -- If hub has no clients and has been inactive for too long, terminate it
                if hub_info.client_count == 0 and last_activity:seconds() > inactivity_duration:seconds() then
                    print("Terminating inactive user hub for", hub_info.hub_pid)
                    local success, err = process.cancel(hub_info.hub_pid, "10s")

                    if success then
                        -- Mark as being terminated to avoid repeated termination attempts
                        hub_info.terminating = true
                        hub_info.termination_started_at = now
                    else
                        print("Failed to terminate hub", hub_info.hub_pid, ":", err)
                    end
                end

                ::continue::
            end
        end
    end

    return actor.new(initial_state, handlers).run()
end

return { run = run }

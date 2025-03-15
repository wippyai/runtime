local time = require("time")
local json = require("json")
local actor = require("actor")

-- Constants
local CENTRAL_HUB_REGISTRY_NAME = "central_hub"
local USER_HUB_REGISTRY_PREFIX = "user_hub."
local WS_JOIN_TOPIC = "ws.join"
local WS_CONTROL_TOPIC = "ws.control"
local USER_HUB_PROCESS_ID = "app.users.relay:user_hub"
local USER_HUB_HOST = "app:processes"
local USER_HUB_INACTIVITY_TIMEOUT = "5s"
local USER_HUB_GC_INTERVAL = "1s"

-- Central Hub Process - Central hub that manages user-specific hubs
local function run()
    -- Define the initial state
    local initial_state = {
        user_hubs = {},
        total_hubs = 0
    }

    -- Define handlers for different message topics
    local handlers = {
        -- Initialize the actor
        __init = function(state)
            -- Register this process with a name for easy discovery
            process.registry.register(CENTRAL_HUB_REGISTRY_NAME)
            print("Central Hub started with PID:", process.pid())

            return state
        end,

        -- Handle system events
        __on_event = function(state, event)
            if event.kind == process.event.EXIT then
                -- A monitored process has exited
                local from_pid = event.from

                print("EVENT")

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

            print("Central Hub shutting down")
            return actor.exit({ status = "shutdown", hubs = state.total_hubs })
        end,

        -- Handle user hub exit messages
        [WS_JOIN_TOPIC] = function(state, payload)
            handle_client_connection(state, payload.client_pid, payload.metadata)
            return state
        end,
    }

    ---- Helper function to extract user_id from metadata
    local function extract_user_id(metadata)
        if type(metadata) ~= "table" then
            return nil
        end
        return metadata.user_id
    end

    ---- Function to create a new user hub for a specific user
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
            state.user_hubs[user_id].connected_clients = (state.user_hubs[user_id].connected_clients or 0) + 1
        end
    end

    --
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
                inactivity_timeout = USER_HUB_INACTIVITY_TIMEOUT
            }
        )

        if not hub_pid then
            print("Failed to create user hub for", user_id, ":", err)
            return nil
        end

        -- Store the hub information
        state.user_hubs[user_id] = {
            hub_pid = hub_pid,
            connected_clients = 0
        }

        state.total_hubs = state.total_hubs + 1
        print("Created user hub for", user_id, "with PID:", hub_pid)

        return hub_pid
    end

    return actor.new(initial_state, handlers).run()
end

return { run = run }

local time = require("time")
local json = require("json")
local actor = require("actor")
local uuid = require("uuid")
local funcs = require("funcs")

-- Registry constants
local USER_HUB_REGISTRY_PREFIX = "user_hub."

-- WebSocket topics
local WS_JOIN_TOPIC = "ws.join"
local WS_LEAVE_TOPIC = "ws.leave"
local WS_MESSAGE_TOPIC = "ws.message"
local WS_CANCEL_TOPIC = "ws.cancel"
local WELCOME_TOPIC = "welcome"
local UPDATE_TOPIC = "update"
local STATS_PING_TOPIC = "stats.ping"
local ERROR_TOPIC = "error" -- New error topic for sending errors

-- Session constants
local SESSION_PROCESS_ID = "wippy.session:session"
local SESSION_HOST = "app:processes"
local SESSION_MESSAGE_TOPIC = "session.message"
local SESSION_COMMAND_TOPIC = "session.command"
local MAX_SESSIONS_PER_USER = 3 -- Maximum allowed sessions per user

-- User Hub Process - Handles WebSocket connections and session management
local function run(args)
    -- Verify required arguments
    if not args or not args.user_id then
        return { error = "Missing required arguments" }
    end

    local user_id = args.user_id
    local user_metadata = args.user_metadata or {}
    local central_hub_pid = args.central_hub_pid

    -- Initialize actor state
    local initial_state = {
        user_id = user_id,
        metadata = user_metadata,
        connected_clients = {},
        client_count = 0,
        central_hub_pid = central_hub_pid,
        active_sessions = {}, -- Map of session_id -> session_pid
        session_count = 0     -- Counter for active sessions
    }

    -- Define handlers for different message topics
    local handlers = {
        -- Initialize the actor
        __init = function(state)
            -- Register this process with the user's ID for easy discovery
            local registry_name = USER_HUB_REGISTRY_PREFIX .. state.user_id
            process.registry.register(registry_name)

            -- Set process options to trap links for session monitoring
            process.set_options({ trap_links = true })

            return state
        end,

        -- Handle process termination events (especially for linked sessions)
        __on_event = function(state, event)
            if event.kind == process.event.LINK_DOWN or event.kind == process.event.EXIT then
                -- Find and remove the terminated session
                for session_id, pid in pairs(state.active_sessions) do
                    if pid == event.from then
                        state.active_sessions[session_id] = nil
                        state.session_count = state.session_count - 1
                        broadcast_to_clients(state, UPDATE_TOPIC, {
                            type = "session_closed",
                            session_id = session_id,
                            reason = "process_terminated",
                            active_session_ids = get_active_session_ids(state)
                        })
                        break
                    end
                end
            end
            return state
        end,

        -- Handle cancellation
        __on_cancel = function(state)
            -- Cancel all active sessions
            for session_id, pid in pairs(state.active_sessions) do
                process.cancel(pid)
            end

            -- Notify clients about shutdown
            broadcast_to_clients(state, WS_CANCEL_TOPIC, {
                type = "system",
                message = "Hub shutting down"
            })

            return actor.exit({ status = "shutdown" })
        end,

        -- Handle WebSocket join
        [WS_JOIN_TOPIC] = function(state, payload)
            local client_pid = payload.client_pid
            state.connected_clients[client_pid] = true
            state.client_count = state.client_count + 1

            -- Send welcome message with user ID and sessions
            process.send(client_pid, WELCOME_TOPIC, {
                user_id = state.user_id,
                client_count = state.client_count,
                active_sessions = state.session_count,
                active_session_ids = get_active_session_ids(state)
            })

            -- Notify central hub about client count change
            if state.central_hub_pid then
                process.send(state.central_hub_pid, STATS_PING_TOPIC, {
                    user_id = state.user_id,
                    client_count = state.client_count,
                    last_activity = time.now():format_rfc3339()
                })
            end

            return state
        end,

        -- Handle WebSocket leave
        [WS_LEAVE_TOPIC] = function(state, payload)
            local client_pid = payload.client_pid
            if state.connected_clients[client_pid] then
                state.connected_clients[client_pid] = nil
                state.client_count = state.client_count - 1

                -- Notify central hub about client count change
                if state.central_hub_pid then
                    process.send(state.central_hub_pid, STATS_PING_TOPIC, {
                        user_id = state.user_id,
                        client_count = state.client_count,
                        last_activity = time.now():format_rfc3339()
                    })
                end
            end
            return state
        end,

        -- Handle WebSocket messages
        [WS_MESSAGE_TOPIC] = function(state, payload, topic, from)
            local message_data, err = json.decode(payload)
            if not message_data then
                -- Send error back to the specific client that sent the malformed message
                process.send(from, ERROR_TOPIC, {
                    type = "error",
                    error = "invalid_json",
                    message = "Failed to decode JSON message"
                })
                return state
            end

            local msg_type = message_data.type
            local session_id = message_data.session_id
            local data = message_data.data

            -- Route message based on type
            if msg_type == "session_open" then
                -- Check if maximum session limit is reached
                if state.session_count >= MAX_SESSIONS_PER_USER then
                    process.send(from, ERROR_TOPIC, {
                        type = "error",
                        error = "session_limit_reached",
                        message = "Maximum session limit reached (" ..
                        MAX_SESSIONS_PER_USER .. " sessions). Please close an existing session.",
                        active_session_ids = get_active_session_ids(state)
                    })
                    return state
                end

                -- Generate session_id if not provided
                if not session_id then
                    session_id = uuid.v4()
                end

                -- Generate a context ID for tracking
                local context_id = uuid.v4()

                -- Create session init data
                local session_init = {
                    session_id = session_id,
                    user_id = state.user_id,
                    parent_pid = process.pid(),
                    conn_pid = process.pid(),
                    start_token = message_data.context,
                    start_context = message_data.start_token,
                }

                -- Spawn session process
                local session_pid, err = process.spawn_linked(
                    SESSION_PROCESS_ID,
                    SESSION_HOST,
                    session_init
                )

                if err then
                    process.send(from, ERROR_TOPIC, {
                        type = "error",
                        error = "session_spawn_error",
                        message = "Failed to create session: " .. err
                    })
                    return state
                end

                if session_pid then
                    state.active_sessions[session_id] = session_pid
                    state.session_count = state.session_count + 1
                    broadcast_to_clients(state, UPDATE_TOPIC, {
                        type = "session_opened",
                        session_id = session_id,
                        context_id = context_id,
                        active_session_ids = get_active_session_ids(state)
                    })
                end
            elseif msg_type == "session_close" then
                -- Validate session ID is provided
                if not session_id then
                    process.send(from, ERROR_TOPIC, {
                        type = "error",
                        error = "invalid_session_id",
                        message = "Session ID is required for closing a session"
                    })
                    return state
                end

                -- Close session if it exists
                local session_pid = state.active_sessions[session_id]
                if session_pid then
                    process.cancel(session_pid)
                    state.active_sessions[session_id] = nil
                    state.session_count = state.session_count - 1
                    broadcast_to_clients(state, UPDATE_TOPIC, {
                        type = "session_closed",
                        session_id = session_id,
                        active_session_ids = get_active_session_ids(state)
                    })
                else
                    -- Send error when trying to close non-existent session
                    process.send(from, ERROR_TOPIC, {
                        type = "error",
                        error = "session_not_found",
                        message = "Cannot close session: session ID not found or invalid"
                    })
                end
            elseif msg_type == "session_message" then
                -- Validate session ID is provided
                if not session_id then
                    process.send(from, ERROR_TOPIC, {
                        type = "error",
                        error = "invalid_session_id",
                        message = "Session ID is required for sending a message"
                    })
                    return state
                end

                -- Forward message to session if it exists
                local session_pid = state.active_sessions[session_id]
                if session_pid then
                    process.send(session_pid, SESSION_MESSAGE_TOPIC, data)
                else
                    process.send(from, ERROR_TOPIC, {
                        type = "error",
                        error = "session_not_found",
                        message = "Session not found: " .. session_id
                    })
                end
            elseif msg_type == "session_command" then
                -- Validate session ID is provided
                if not session_id then
                    process.send(from, ERROR_TOPIC, {
                        type = "error",
                        error = "invalid_session_id",
                        message = "Session ID is required for sending a command"
                    })
                    return state
                end

                -- Forward command to session if it exists
                local session_pid = state.active_sessions[session_id]
                if session_pid then
                    process.send(session_pid, SESSION_COMMAND_TOPIC, data)
                else
                    -- Send error when trying to send command to a non-existent session
                    process.send(from, ERROR_TOPIC, {
                        type = "error",
                        error = "session_not_found",
                        message = "Cannot send command: session ID not found or invalid"
                    })
                end
            else
                -- Unrecognized message type
                process.send(from, ERROR_TOPIC, {
                    type = "error",
                    error = "invalid_message_type",
                    message = "Unrecognized message type: " .. (msg_type or "nil")
                })
            end

            return state
        end,

        -- Forward any other messages to clients (including responses from sessions)
        __default = function(state, payload, topic)
            if topic == "__default" then
                return state
            end

            broadcast_to_clients(state, topic, payload)
            return state
        end
    }

    -- Helper function to broadcast a message to all connected clients
    function broadcast_to_clients(state, topic, message)
        for client_pid, _ in pairs(state.connected_clients) do
            process.send(client_pid, topic, message)
        end
    end

    -- Helper function to get all active session IDs as an array
    function get_active_session_ids(state)
        local session_ids = {}
        for session_id, _ in pairs(state.active_sessions) do
            table.insert(session_ids, session_id)
        end
        return session_ids
    end

    -- Create and run the actor
    local user_hub_actor = actor.new(initial_state, handlers)
    return user_hub_actor.run()
end

return { run = run }
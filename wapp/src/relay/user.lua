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

-- Context constants
local CONTEXT_CREATE_FUNCTION = "app.sessions:create_context"

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
        active_sessions = {} -- Map of session_id -> session_pid
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
                        broadcast_to_clients(state, UPDATE_TOPIC, {
                            type = "session_closed",
                            session_id = session_id,
                            reason = "process_terminated"
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
                active_sessions = #state.active_sessions,
            })

            return state
        end,

        -- Handle WebSocket leave
        [WS_LEAVE_TOPIC] = function(state, payload)
            local client_pid = payload.client_pid
            if state.connected_clients[client_pid] then
                state.connected_clients[client_pid] = nil
                state.client_count = state.client_count - 1
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
                -- Generate session_id if not provided
                if not session_id then
                    session_id = uuid.v4()
                end

                -- Check for context - it's always required
                local context_payload = message_data.context or {}
                local start_token = message_data.start_token

                -- If no context_id but context payload exists, we need to create context
                -- Initialize context in the registry
                local context_result, err = funcs.new():call(
                    CONTEXT_CREATE_FUNCTION,
                    {
                        user_id = state.user_id,
                        data = context_payload,
                        type = "data"
                    }
                )

                if err then
                    -- Send error only to the originating client
                    process.send(from, ERROR_TOPIC, {
                        type = "error",
                        error = "context_creation_failed",
                        message = "Failed to create context: " .. err
                    })
                    return state
                end

                if not context_result or not context_result.context_id then
                    -- Send error only to the originating client
                    process.send(from, ERROR_TOPIC, {
                        type = "error",
                        error = "context_creation_failed",
                        message = "Failed to get context ID from result"
                    })
                    return state
                end

                local context_id = context_result.context_id

                -- Create session init data
                local session_init = {
                    session_id = session_id,
                    user_id = state.user_id,
                    parent_pid = process.pid(),
                    conn_pid = process.pid(),
                    primary_context_id = context_id,
                    kind = message_data.kind or "default",
                    start_token = start_token
                }

                -- Spawn session process
                local session_pid = process.spawn_linked(
                    SESSION_PROCESS_ID,
                    SESSION_HOST,
                    session_init
                )

                if session_pid then
                    state.active_sessions[session_id] = session_pid
                    broadcast_to_clients(state, UPDATE_TOPIC, {
                        type = "session_opened",
                        session_id = session_id,
                        context_id = context_id
                    })
                end
            elseif msg_type == "session_close" then
                -- Close session if it exists
                local session_pid = state.active_sessions[session_id]
                if session_pid then
                    process.cancel(session_pid)
                    state.active_sessions[session_id] = nil
                    broadcast_to_clients(state, UPDATE_TOPIC, {
                        type = "session_closed",
                        session_id = session_id
                    })
                end
            elseif msg_type == "session_message" and session_id then
                -- Forward message to session if it exists
                local session_pid = state.active_sessions[session_id]
                if session_pid then
                    process.send(session_pid, SESSION_MESSAGE_TOPIC, data)
                end
            elseif msg_type == "session_command" and session_id then
                -- Forward command to session if it exists
                local session_pid = state.active_sessions[session_id]
                if session_pid then
                    process.send(session_pid, SESSION_COMMAND_TOPIC, data)
                end
            end

            return state
        end,

        -- Forward any other messages to clients (including responses from sessions)
        __default = function(state, payload, topic)
            -- todo: we can filter it out a bit
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

    -- Create and run the actor
    local user_hub_actor = actor.new(initial_state, handlers)
    return user_hub_actor.run()
end

return { run = run }
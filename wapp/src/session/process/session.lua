-- session.lua
local time = require("time")
local json = require("json")
local actor = require("actor")
local session_state = require("session_state")
local loader = require("loader")

-- Topic constants
local INIT_TOPIC = "init"
local SESSION_MESSAGE_TOPIC = "session.message"
local SESSION_COMMAND_TOPIC = "session.command"
local MESSAGE_TYPE_SESSION_READY = "session_ready"

local function run(args)
    -- Validate required args
    if not args or not args.user_id or not args.session_id then
        error("User ID and session ID are required")
    end

    -- Create/load session via loader - this gives us the base state
    local loader_state, err
    if args.create then
        if not args.start_token then
            error("Start token is required for new session")
        end
        loader_state, err = loader.create_session(args)
    else
        loader_state, err = loader.load_session(args)
    end

    if err then
        error(err)
    end

    -- Create session state object using the loader state data
    local session = session_state.new(loader_state)

    -- Set connection and parent process IDs
    session:set_conn_pid(args.conn_pid)
    session:set_parent_pid(args.parent_pid)

    -- If creating new session with agent from loader_state meta
    if args.create and loader_state.meta and loader_state.meta.agent then
        local _, err = session:initialize_with_agent_name(loader_state.meta.agent, loader_state.meta.model)
        if err then
            error("Failed to initialize agent: " .. err)
        end
    else
        -- For existing sessions, load message history
        local _, err = session:load_history()
        if err then
            error("Failed to load message history: " .. err)
        end
    end

    -- Notify client that session is ready
    if args.conn_pid then
        process.send(args.conn_pid, INIT_TOPIC, {
            type = MESSAGE_TYPE_SESSION_READY,
            session_id = loader_state.session_id,
            agent = loader_state.meta and loader_state.meta.agent,
            model = loader_state.meta and loader_state.meta.model,
            status = loader_state.status,
            last_message_id = loader_state.last_message_id
        })
    end

    -- Define message handlers
    local handlers = {
        -- Handle cancellation
        __on_cancel = function(state)
            print("Session cancelled:", state.session_id)
            return actor.exit({ status = "shutdown" })
        end,

        -- Handle user messages
        [SESSION_MESSAGE_TOPIC] = function(state, payload)
            if not payload or not payload.data then
                return state
            end

            -- Update conn_pid if provided
            if payload.conn_pid then
                session:set_conn_pid(payload.conn_pid)
            end

            -- Process the message
            local result, err = session:process_message(payload.data)

            if err then
                print("Error processing message:", err)
                return state
            end

            -- Execute agent asynchronously
            coroutine.spawn(function()
                local exec_result, exec_err = session:execute_agent(result)

                if exec_err then
                    print("Error executing agent:", exec_err)
                end
            end)

            return state
        end,

        -- Handle commands
        [SESSION_COMMAND_TOPIC] = function(state, payload)
            if not payload or not payload.command then
                return state
            end

            -- Update conn_pid if provided
            if payload.conn_pid then
                session:set_conn_pid(payload.conn_pid)
            end

            local command = payload.command

            if command == "stop" then
                session:stop_generation()
            elseif command == "model" then
                if payload.data and payload.data.name then
                    session:change_model(payload.data.name)
                end
            elseif command == "agent" then
                -- Switch to a different agent by name
                if payload.data and payload.data.name then
                    session:change_agent(payload.data.name)
                end
            end

            return state
        end
    }

    -- Use loader_state as the initial actor state
    return actor.new(loader_state, handlers).run()
end

return { run = run }

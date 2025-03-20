local json = require("json")
local actor = require("actor")
local session_state = require("session_state")
local loader = require("loader")
local session_updater = require("session_updater")

-- Topic constants
local MESSAGE_TOPIC = "message"
local COMMAND_TOPIC = "command"

-- Session status constants
local STATUS = {
    IDLE = "idle",
    RUNNING = "running",
    ERROR = "error",
    FAILED = "failed"
}

-- Command constants
local CMD = {
    STOP = "stop",
    MODEL = "model",
    AGENT = "agent"
}

-- Error code constants
local ERROR_CODE = {
    FAILED = "FAILED",
    ERROR = "ERROR",
    BUSY = "BUSY",
    AGENT_ERROR = "AGENT_ERROR",
    DB_ERROR = "DB_ERROR"
}

-- Error message constants
local ERR = {
    MISSING_ARGS = "User ID and session ID are required",
    MISSING_TOKEN = "Start token is required for new session",
    FAILED_STATE = "Session is in failed state and cannot process messages",
    FAILED_COMMANDS = "Session is in failed state and cannot process commands",
    INIT_FAILED = "Session initialization failed",
    EXEC_AGENT = "Error executing agent",
    BUSY = "Session is already processing a message",
    UNSUPPORTED_COMMAND = "Unsupported command"
}

local function run(args)
    -- Validate required args
    if not args or not args.user_id or not args.session_id then
        error(ERR.MISSING_ARGS)
    end

    -- Create/load session via loader
    local loader_state, err
    if args.create then
        if not args.start_token then
            error(ERR.MISSING_TOKEN)
        end
        loader_state, err = loader.create_session(args)
    else
        loader_state, err = loader.load_session(args)
    end

    if err then
        error(err)
    end

    -- Initialize session status from loader_state
    local session_status = loader_state.status or STATUS.IDLE

    -- Create an updater instance
    local updater = session_updater.new(args.session_id, args.conn_pid, args.parent_pid)

    -- Create session state object using the loader state data
    -- Pass the updater by reference so we can update it without notifying session_state
    local state = session_state.new(loader_state, updater)

    -- Check if the session is already in a failed state
    if session_status == STATUS.FAILED then
        updater:session_error(ERROR_CODE.FAILED, ERR.INIT_FAILED)
        error("Unable to open failed session")
    end

    -- Normal session initialization
    if args.create and loader_state.meta and loader_state.meta.agent then
        -- Initialize with agent name
        local success, init_err = state:initialize_with_agent_name(loader_state.meta.agent, loader_state.meta.model)
        if not success then
            -- Session state has already updated the DB status
            session_status = STATUS.FAILED
            updater:session_error(ERROR_CODE.FAILED, init_err)
            error(init_err)
        end
    else
        -- For existing sessions, load message history
        local success, history_err = state:load_history()
        if not success then
            -- Session state has already updated the DB status
            session_status = STATUS.FAILED
            state:update_session_status(STATUS.FAILED, history_err)
            updater:session_error(ERROR_CODE.FAILED, history_err)
            error(history_err)
        end
    end

    -- Notify client that session is ready using updater
    updater:update_session({
        agent = loader_state.meta and loader_state.meta.agent,
        model = loader_state.meta and loader_state.meta.model,
        status = session_status,
        last_message_date = loader_state.last_message_date,
    })

    -- Flag to track generation stop requests
    local stop_requested = false

    -- Define message handlers
    local handlers = {
        -- Handle cancellation
        __on_cancel = function(actor_state)
            -- todo: wait for agent to finish before exiting if it's still working
            return actor.exit({ status = "shutdown" })
        end,

        -- Handle unhandled messages
        __default = function(actor_state, payload)
            print("unhandled message:", json.encode(payload))
            return actor_state
        end,

        -- Handle user messages
        [MESSAGE_TOPIC] = function(actor_state, payload)
            if not payload or not payload.data then
                return actor_state
            end

            -- Update connection PID if provided
            if payload.conn_pid then
                updater.conn_pid = payload.conn_pid
            end

            -- Don't process messages if session is in failed state
            if session_status == STATUS.FAILED then
                updater:session_error(ERROR_CODE.FAILED, ERR.FAILED_STATE)
                return actor_state
            end

            -- Don't process if already running
            if session_status == STATUS.RUNNING then
                updater:session_error(ERROR_CODE.BUSY, ERR.BUSY)
                return actor_state
            end

            -- Reset stop flag when starting a new message
            stop_requested = false

            -- Update session status via session_state
            session_status = STATUS.RUNNING
            state:update_session_status(STATUS.RUNNING)
            updater:update_session({ status = STATUS.RUNNING })

            -- Process the message
            local next, msg_err = state:process_message(payload.data)
            if not next then
                -- Handle error, reset status via session_state
                session_status = STATUS.ERROR
                state:update_session_status(STATUS.ERROR)

                -- Notify clients about the error
                updater:session_error(ERROR_CODE.ERROR, msg_err)

                print("Error processing message:", msg_err)
                return actor_state
            end

            -- Execute agent asynchronously
            actor_state.async(function()
                -- Check if stop requested before execution
                if stop_requested then
                    session_status = STATUS.IDLE
                    state:update_session_status(STATUS.IDLE)
                    updater:update_session({ status = STATUS.IDLE })
                    return { stopped = true }
                end

                local exec_result, exec_err = state:execute_agent(next, stop_requested)
                if not exec_result then
                    print("error executing agent:", exec_err)
                    return nil, exec_err
                end

                return exec_result
            end).on_complete(function(actor_state, result, error)
                if error then
                    print("async execution error:", error)
                    session_status = STATUS.ERROR
                    state:update_session_status(STATUS.ERROR)
                    updater:session_error(ERROR_CODE.ERROR, ERR.EXEC_AGENT .. ": " .. error)

                    -- todo: we probably can retry, right?

                    return
                end

                -- Always update to idle state when complete, whether stopped or not
                session_status = STATUS.IDLE
                state:update_session_status(STATUS.IDLE)
                updater:update_session({ status = STATUS.IDLE })
            end)

            return actor_state
        end,

        -- Handle commands
        [COMMAND_TOPIC] = function(actor_state, payload)
            if not payload or not payload.command then
                return actor_state
            end

            -- Don't process commands if session is in failed state
            if session_status == STATUS.FAILED then
                updater:session_error(ERROR_CODE.FAILED, ERR.FAILED_COMMANDS)
                return actor_state
            end

            local command = payload.command

            if command == CMD.STOP then
                -- Set the stop flag to signal the agent to stop
                stop_requested = true

                -- If we're not in RUNNING state, also update status via session_state
                if session_status ~= STATUS.RUNNING then
                    session_status = STATUS.IDLE
                    state:update_session_status(STATUS.IDLE)

                    updater:update_session({
                        status = STATUS.IDLE
                    })
                end
            elseif command == CMD.MODEL then
                if  payload.name then
                    local success, err = state:change_model(payload.name)
                    if not success then
                        updater:session_error(ERROR_CODE.ERROR, err)
                    end
                end
            elseif command == CMD.AGENT then
                if payload.name then
                    local success, err = state:change_agent(payload.name)
                    if not success then
                        updater:session_error(ERROR_CODE.ERROR, err)
                    end
                end
            else
                updater:session_error(ERROR_CODE.ERROR, ERR.UNSUPPORTED_COMMAND)
            end

            return actor_state
        end,
    }

    -- Use loader_state as the initial actor state
    return actor.new(loader_state, handlers).run()
end

return { run = run }

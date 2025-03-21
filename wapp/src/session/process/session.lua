local json = require("json")
local actor = require("actor")
local session_state = require("session_state")
local controller = require("controller")
local loader = require("loader")
local session_upstream = require("session_upstream")

-- Topic constants
local MESSAGE_TOPIC = "message"
local COMMAND_TOPIC = "command"
local CONTINUE_TOPIC = "continue"

-- Session status constants
local STATUS = {
    IDLE = "idle",
    RUNNING = "running",
    ERROR = "error",
    FAILED = "failed"
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

    -- Create session state object using the loader state data
    local state = session_state.new(loader_state)

    -- Create an upstream instance
    local upstream = session_upstream.new(args.session_id, args.conn_pid, args.parent_pid)

    -- Create controller instance that will manage conversation flow
    local convo_controller = controller.new(state, upstream, {
        -- Callback for controller to request continuation
        ask_continue = function(payload)
            -- Post to the continue topic to trigger further processing
            actor.post(CONTINUE_TOPIC, payload)
        end
    })

    -- Set session status (centralized status management)
    local function set_session_status(new_status, error_msg)
        -- Update local variable
        session_status = new_status

        -- Update state
        state:update_session_status(new_status, error_msg)

        -- Notify clients
        if error_msg then
            upstream:session_error(new_status == STATUS.FAILED and ERROR_CODE.FAILED or ERROR_CODE.ERROR, error_msg)
        else
            upstream:update_session({ status = new_status })
        end
    end

    -- Check if the session is already in a failed state
    if session_status == STATUS.FAILED then
        upstream:session_error(ERROR_CODE.FAILED, ERR.INIT_FAILED)
        error("Unable to open failed session")
    end

    -- Normal session initialization
    if args.create and loader_state.meta and loader_state.meta.agent then
        -- Set agent and model through controller
        local success, init_err = convo_controller:init(
            loader_state.meta.agent,
            loader_state.meta.model
        )

        if not success then
            -- Session state has already updated the DB status
            session_status = STATUS.FAILED
            upstream:session_error(ERROR_CODE.FAILED, init_err)
            error(init_err)
        end
    else
        -- For existing sessions, load message history through state
        local success, history_err = state:load_history()
        if not success then
            -- Session state has already updated the DB status
            session_status = STATUS.FAILED
            state:update_session_status(STATUS.FAILED, history_err)
            upstream:session_error(ERROR_CODE.FAILED, history_err)
            error(history_err)
        end
    end

    -- Notify client that session is ready using upstream
    upstream:update_session({
        agent = loader_state.meta and loader_state.meta.agent,
        model = loader_state.meta and loader_state.meta.model,
        status = session_status,
        last_message_date = loader_state.last_message_date,
    })

    -- Define message handlers
    local handlers = {
        -- Handle cancellation
        __on_cancel = function(actor_state)
            convo_controller:cancel()
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

            -- Don't process messages if session is in failed state
            if session_status == STATUS.FAILED then
                upstream:session_error(ERROR_CODE.FAILED, ERR.FAILED_STATE)
                return actor_state
            end

            -- Don't process if already running
            if session_status == STATUS.RUNNING then
                upstream:session_error(ERROR_CODE.BUSY, ERR.BUSY)
                return actor_state
            end

            -- Update connection PID if provided
            if payload.conn_pid then
                upstream.conn_pid = payload.conn_pid
            end

            payload.type = controller.CMD.MESSAGE
            return actor.next(CONTINUE_TOPIC, payload)
        end,

        -- Handle commands
        [COMMAND_TOPIC] = function(actor_state, payload)
            if not payload or not payload.command then
                return actor_state
            end

            -- Update connection PID if provided
            if payload.conn_pid then
                upstream.conn_pid = payload.conn_pid
            end

            -- Don't process commands if session is in failed state
            if session_status == STATUS.FAILED then
                upstream:session_error(ERROR_CODE.FAILED, ERR.FAILED_COMMANDS)
                return actor_state
            end

            -- Handle command through controller
            local success, err = convo_controller:handle_command(payload.command, payload)

            if not success then
                upstream:session_error(ERROR_CODE.ERROR, err or "Command failed")
            end

            return actor_state
        end,

        -- Handle controller-initiated continue actions
        [CONTINUE_TOPIC] = function(actor_state, payload)
            if session_status == STATUS.RUNNING or session_status == STATUS.FAILED then
                return actor_state
            end

            -- Set status to running
            set_session_status(STATUS.RUNNING)

            -- Process continue action asynchronously
            actor_state.async(function()
                local result, err

                if payload.type == controller.CMD.MESSAGE then
                    -- Handle user message
                    result, err = convo_controller:handle_message(payload.data)
                else
                    -- Handle controller-initiated action
                    result, err = convo_controller:continue(payload)
                end

                if err then
                    print("Error in processing:", err)
                    set_session_status(STATUS.ERROR, err)
                    return
                end

                -- Update session status after processing
                set_session_status(STATUS.IDLE)
            end)

            return actor_state
        end,
    }

    -- Use loader_state as the initial actor state
    return actor.new(loader_state, handlers).run()
end

return { run = run }

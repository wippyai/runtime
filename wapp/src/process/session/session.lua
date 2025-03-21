local json = require("json")
local actor = require("actor")
local loader = require("loader")
local controller = require("controller")
local session_state = require("session_state")
local session_upstream = require("session_upstream")
local meta_context = require("meta_context")

local MESSAGE_TOPIC = "message"
local COMMAND_TOPIC = "command"
local CONTINUE_TOPIC = "continue"
local CONTEXT_TOPIC = "context"
local TITLE_TOPIC = "title"
local METADATA_TOPIC = "metadata"

local STATUS = {
    IDLE = "idle",
    RUNNING = "running",
    ERROR = "error",
    FAILED = "failed"
}

local ERROR_CODE = {
    FAILED = "FAILED",
    ERROR = "ERROR",
    BUSY = "BUSY",
    AGENT_ERROR = "AGENT_ERROR",
    DB_ERROR = "DB_ERROR"
}

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
    if not args or not args.user_id or not args.session_id then
        error(ERR.MISSING_ARGS)
    end

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

    local session_status = loader_state.status or STATUS.IDLE

    local state = session_state.new(loader_state)
    local upstream = session_upstream.new(args.session_id, args.conn_pid, args.parent_pid)
    local convo_controller = controller.new(state, upstream)
    local ctx_manager = meta_context.new(state, upstream)

    local function set_session_status(new_status, error_msg)
        session_status = new_status
        state:update_session_status(new_status, error_msg)

        if error_msg then
            upstream:session_error(new_status == STATUS.FAILED and ERROR_CODE.FAILED or ERROR_CODE.ERROR, error_msg)
        else
            upstream:update_session({ status = new_status })
        end
    end

    if session_status == STATUS.FAILED then
        upstream:session_error(ERROR_CODE.FAILED, ERR.INIT_FAILED)
        error("Unable to open failed session")
    end

    if args.create and loader_state.meta and loader_state.meta.agent then
        local success, init_err = convo_controller:init(
            loader_state.meta.agent,
            loader_state.meta.model
        )

        if not success then
            session_status = STATUS.FAILED
            upstream:session_error(ERROR_CODE.FAILED, init_err)
            error(init_err)
        end
    else
        local messages, history_err = state:load_messages(50)
        if history_err then
            session_status = STATUS.FAILED
            state:update_session_status(STATUS.FAILED, history_err)
            upstream:session_error(ERROR_CODE.FAILED, history_err)
            error(history_err)
        end
    end

    upstream:update_session({
        agent = loader_state.meta and loader_state.meta.agent,
        model = loader_state.meta and loader_state.meta.model,
        status = session_status,
        last_message_date = loader_state.last_message_date,
    })

    -- Function to start title generation coroutine
    local function generate_title()
        local title = "Generated title!"

        -- Update title in state
        local success, err = self.state:update_session_title(title)
        if not success then
            return false, err
        end

        -- Notify clients about title update
        upstream:update_session({
            title = title
        })
    end

    -- Handler for context commands
    local function handle_context_command(payload)
        if payload.action == "write" then
            if not payload.key then
                return false, "Context key is required"
            end
            return ctx_manager:write_context(payload.key, payload.data)
        elseif payload.action == "delete" then
            if not payload.key then
                return false, "Context key is required"
            end
            return ctx_manager:delete_context(payload.key)
        else
            return false, "Invalid context action"
        end
    end

    -- Handler for metadata commands
    local function handle_metadata_command(payload)
        if payload.action == "update" then
            if not payload.items then
                return false, "Metadata items are required"
            end
            return ctx_manager:update_public_metadata(payload.items)
        elseif payload.action == "remove" then
            if not payload.ids then
                return false, "Metadata IDs are required"
            end
            return ctx_manager:remove_public_metadata(payload.ids)
        else
            return false, "Invalid metadata action"
        end
    end

    -- Handler for title commands
    local function handle_title_command(payload)
        if not payload.title then
            return false, "Title is required"
        end
        return ctx_manager:update_title(payload.title)
    end

    local handlers = {
        __on_cancel = function(actor_state)
            convo_controller:cancel()
            return actor.exit({ status = "shutdown" })
        end,

        __default = function(actor_state, payload)
            print("unhandled message:", json.encode(payload))
            return actor_state
        end,

        [MESSAGE_TOPIC] = function(actor_state, payload)
            if not payload or not payload.data then
                return actor_state
            end

            if session_status == STATUS.FAILED then
                upstream:session_error(ERROR_CODE.FAILED, ERR.FAILED_STATE)
                return actor_state
            end

            if session_status == STATUS.RUNNING then
                upstream:session_error(ERROR_CODE.BUSY, ERR.BUSY)
                return actor_state
            end

            if payload.conn_pid then
                upstream.conn_pid = payload.conn_pid
            end

            payload.type = controller.CMD.MESSAGE
            return actor.next(CONTINUE_TOPIC, payload)
        end,

        [COMMAND_TOPIC] = function(actor_state, payload)
            if not payload or not payload.command then
                return actor_state
            end

            if payload.conn_pid then
                upstream.conn_pid = payload.conn_pid
            end

            if session_status == STATUS.FAILED then
                upstream:session_error(ERROR_CODE.FAILED, ERR.FAILED_COMMANDS)
                return actor_state
            end

            local success, err
            -- Handle special session-level commands directly
            if payload.command == CONTEXT_TOPIC then
                success, err = handle_context_command(payload)
            elseif payload.command == METADATA_TOPIC then
                success, err = handle_metadata_command(payload)
            elseif payload.command == TITLE_TOPIC then
                success, err = handle_title_command(payload)
            else
                -- Pass other commands to controller
                success, err = convo_controller:handle_command(payload.command, payload)
            end

            if success then
                -- Send command_success for commands with request_id
                if payload.request_id then
                    upstream:command_success(payload.request_id)
                end
            else
                if payload.request_id then
                    upstream:command_error(payload.request_id, ERROR_CODE.ERROR, err or "Command failed")
                end

                upstream:session_error(ERROR_CODE.ERROR, err or "Command failed")
            end

            return actor_state
        end,

        [CONTINUE_TOPIC] = function(actor_state, payload)
            if session_status == STATUS.RUNNING or session_status == STATUS.FAILED then
                return actor_state
            end

            set_session_status(STATUS.RUNNING)

            actor_state.async(function()
                local result, err

                if payload.type == controller.CMD.MESSAGE then
                    result, err = convo_controller:handle_message(payload.data)

                    -- If message was successful and we have enough messages, start title generation
                    if not err and result then
                        local user_count = state:count_messages_by_type("user")
                        if user_count >= 3 and (not state.title or state.title == "") then
                            generate_title()
                        end
                    end
                else
                    result, err = convo_controller:continue(payload)
                end

                if err then
                    print("Error in processing:", err)
                    set_session_status(STATUS.ERROR, err)
                    return
                end

                set_session_status(STATUS.IDLE)

                if convo_controller.next_payload then
                    return actor.next(CONTINUE_TOPIC, convo_controller.next_payload)
                end
            end)

            return actor_state
        end,
    }

    return actor.new(loader_state, handlers).run()
end

return { run = run }

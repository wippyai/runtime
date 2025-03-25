local json = require("json")
local actor = require("actor")
local consts = require("consts")
local loader = require("loader")
local controller = require("controller")
local session_state = require("session_state")
local session_upstream = require("session_upstream")
local session_context = require("session_context")

-- Use constants from consts package
local STATUS = consts.STATUS
local TOPICS = consts.TOPICS
local ERROR_CODE = consts.ERROR_CODE
local ERR = consts.ERR
local TASK_TYPE = consts.TASK_TYPE

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
    local ctx_manager = session_context.new(loader_state.primary_context_id)
    local upstream = session_upstream.new(args.session_id, args.conn_pid, args.parent_pid)
    local convo_controller = controller.new(state, upstream)

    local function set_session_status(new_status, error_msg)
        session_status = new_status
        state:update_session_status(new_status, error_msg)

        if error_msg then
            upstream:session_error(new_status == STATUS.FAILED and ERROR_CODE.FAILED or ERROR_CODE.ERROR, error_msg)
        else
            upstream:update_session({ status = new_status })
        end
    end

    -- Function to check for and process any pending work in the controller
    local function check_next_work()
        if convo_controller:has_next() then
            return actor.next(TOPICS.CONTINUE)
        end
        return nil
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
    end

    upstream:update_session({
        agent = loader_state.meta and loader_state.meta.agent,
        model = loader_state.meta and loader_state.meta.model,
        status = session_status,
        last_message_date = loader_state.last_message_date,
        public_meta = loader_state.public_meta
    })

    local title_requested = false
    local function generate_title()
        if title_requested then
            return
        end
        title_requested = true

        local title = "Generated title!"

        -- Update title in state
        local success, err = state:update_session_title(title)
        if not success then
            return false, err
        end

        -- Notify clients about title update
        upstream:update_session({
            title = title
        })

        return true
    end

    -- Handler for public_meta commands
    local function handle_public_meta_command(payload)
        if not payload.public_meta or type(payload.public_meta) ~= "table" then
            return false, "Public metadata must be a table"
        end
            print("OKK")

        -- Update public metadata in state
        local success, err = state:update_session_config({
            public_meta = payload.public_meta
        })
        if not success then
            return false, err
        end

        -- Notify clients about metadata update
        upstream:update_session({
            public_meta = payload.public_meta
        })

        return true
    end

    process.registry.register("session." .. args.session_id)

    local handlers = {
        __on_cancel = function(actor_state)
            convo_controller:cancel()
            return actor.exit({ status = "shutdown" })
        end,

        __default = function(actor_state, payload)
            print("unhandled message:", json.encode(payload))
            return actor_state
        end,

        [TOPICS.MESSAGE] = function(actor_state, payload)
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

            local result, err = convo_controller:handle_message(payload.data)

            if not result then
                if err then
                    upstream:session_error(ERROR_CODE.ERROR, err)
                end
                return actor_state
            end

            return check_next_work()
        end,

        [TOPICS.COMMAND] = function(actor_state, payload, topic, from)
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

            payload.from_pid = from

            -- Handle special session-level commands directly
            if payload.command == TOPICS.CONTEXT then
                success, err = ctx_manager:handle_command(context_payload)
                if success then
                    -- we do no announce this commands normally, they are not public
                    return actor_state
                end
            elseif payload.command == TOPICS.PUBLIC_META then
                success, err = handle_public_meta_command(payload)
            else
                -- Pass other commands to controller
                success, err = convo_controller:handle_command(payload.command, payload)
            end

            if not success then
                if payload.request_id then
                    upstream:command_error(payload.request_id, ERROR_CODE.ERROR, err or "Command failed")
                end

                upstream:session_error(ERROR_CODE.ERROR, err or "Command failed")
                return actor_state
            end

            if payload.request_id then
                upstream:command_success(payload.request_id)
            end

            -- Check if we need to continue processing
            return check_next_work()
        end,

        [TOPICS.CONTINUE] = function(actor_state, payload)
            if session_status == STATUS.FAILED then
                return actor_state
            end

            set_session_status(STATUS.RUNNING)

            actor_state.async(function()
                local result, err = convo_controller:process_next()
                if err then
                    print("error in processing:", err)
                    set_session_status(STATUS.ERROR, err)
                end

                -- If message was successful and we have enough messages, start title generation
                if result and state.total_message_count >= 5
                    and (not state.title or state.title == "")
                    and not title_requested then
                    actor_state.async(generate_title)
                end

                -- Check if we still have pending work
                if convo_controller:has_next() then
                    -- Keep status as RUNNING and continue with next task
                    return actor.next(TOPICS.CONTINUE, true)
                else
                    -- No more work, set status to IDLE
                    set_session_status(STATUS.IDLE)
                end
            end)

            return actor_state
        end,
    }

    return actor.new(loader_state, handlers).run()
end

return { run = run }

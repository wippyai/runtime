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

    -- Check if the session is already in a failed state
    if loader_state.status == "failed" then
        -- Just notify that session is in failed state and continue
        if args.conn_pid then
            process.send(args.conn_pid, INIT_TOPIC, {
                type = MESSAGE_TYPE_SESSION_READY,
                session_id = loader_state.session_id,
                agent = loader_state.meta and loader_state.meta.agent,
                model = loader_state.meta and loader_state.meta.model,
                status = loader_state.status,
                last_message_id = loader_state.last_message_id,
                error = loader_state.error
            })
        end
    else
        -- Normal session initialization
        -- If creating new session with agent from loader_state meta
        if args.create and loader_state.meta and loader_state.meta.agent then
            -- Initialize with agent name - this will mark the session as failed if agent can't be loaded
            local _, err = session:initialize_with_agent_name(loader_state.meta.agent, loader_state.meta.model)
            if err then
                -- Session has already been marked as failed by initialize_with_agent_name
                console.error("Failed to initialize agent: " .. err)
            end
        else
            -- For existing sessions, load message history
            local _, err = session:load_history()
            if err then
                -- Mark session as failed if we can't load history
                session:mark_session_failed("Failed to load message history: " .. err)
                console.error("Failed to load message history: " .. err)
            end
        end

        -- Notify client that session is ready
        if args.conn_pid then
            process.send(args.conn_pid, INIT_TOPIC, {
                type = MESSAGE_TYPE_SESSION_READY,
                session_id = loader_state.session_id,
                agent = loader_state.meta and loader_state.meta.agent,
                model = loader_state.meta and loader_state.meta.model,
                status = session.status, -- Use updated status that might be "failed"
                last_message_id = loader_state.last_message_id,
                error = session.status == "failed" and "Session initialization failed" or nil
            })
        end
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

            -- Don't process messages if session is in failed state
            if session.status == "failed" then
                -- Notify that session is in failed state
                session:broadcast({
                    type = MSG_TYPE.SYSTEM,
                    session_id = session.session_id,
                    status = "failed",
                    message = "Session is in failed state and cannot process messages"
                })
                return state
            end

            -- Process the message
            local result, err = session:process_message(payload.data)

            if err then
                print("Error processing message:", err)
                return state
            end

            -- Execute agent asynchronously using state.async
            state.async(function()
                print("RUNNING AGENT")
                local exec_result, exec_err = session:execute_agent(result)

                if exec_err then
                    print("Error executing agent:", exec_err)
                    return nil, exec_err
                end

                -- Log the result for debugging
                print(require("json").encode(exec_result))

                return exec_result
            end).on_complete(function(state, result, error)
                if error then
                    print("Async execution error:", error)
                    -- Notify clients about the error
                    session:broadcast({
                        type = MSG_TYPE.SYSTEM,
                        session_id = session.session_id,
                        status = STATUS.ERROR,
                        message = "Error executing agent: " .. error
                    })
                    return
                end

                if result then
                    if result.delegation then
                        print("Delegation processed:", require("json").encode(result.delegation))
                        -- Delegation has already been broadcast in execute_agent

                        -- Here we would initiate the delegation to another agent
                        -- For now, just log it
                        console.debug("Delegation to agent:", result.delegation.target)
                    elseif result.tool_calls then
                        print("Tool calls need processing:", require("json").encode(result.tool_calls))

                        -- In the future, we'll process tool calls here
                        -- For each tool call:
                        -- 1. Execute the tool
                        -- 2. Add the result back to the conversation
                        -- 3. Continue the conversation

                        -- For now, just notify that tool calls would be processed
                        session:broadcast({
                            type = MSG_TYPE.SYSTEM,
                            session_id = session.session_id,
                            message = "Tool calls would be processed here"
                        })

                        -- Reset status to idle for now (in the future, tool processing would do this)
                        session.status = STATUS.IDLE
                        session_repo.update_session_meta(session.session_id, { status = STATUS.IDLE })
                    else
                        print("Execution completed:", require("json").encode(result))
                        -- Regular message has already been broadcast in execute_agent
                    end
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

            -- Don't process commands if session is in failed state,
            -- except for the special "reset" command that can recover a failed session
            if session.status == "failed" and payload.command ~= "reset" then
                -- Notify that session is in failed state
                session:broadcast({
                    type = MSG_TYPE.SYSTEM,
                    session_id = session.session_id,
                    status = "failed",
                    message = "Session is in failed state and cannot process commands"
                })
                return state
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
            elseif command == "reset" then
                -- Special command to attempt to recover a failed session
                -- by resetting it to idle and trying to reload everything
                if session.status == "failed" and payload.data and payload.data.agent then
                    -- Try to reset by changing to a specified agent
                    session.status = "idle"
                    session_repo.update_session_meta(
                        session.session_id,
                        { status = "idle", error = nil }
                    )
                    session:change_agent(payload.data.agent)
                end
            end

            return state
        end
    }

    -- Use loader_state as the initial actor state
    return actor.new(loader_state, handlers).run()
end

return { run = run }

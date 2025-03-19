local time = require("time")
local json = require("json")
local actor = require("actor")
local loader = require("loader")

-- Topic constants
local UPDATE_TOPIC = "update"
local SESSION_MESSAGE_TOPIC = "session.message"
local SESSION_COMMAND_TOPIC = "session.command"

-- Session status and message type constants
local MESSAGE_TYPE_START = "start"
local MESSAGE_TYPE_THINKING = "thinking"
local MESSAGE_TYPE_CONTENT = "content"
local MESSAGE_TYPE_DONE = "done"
local MESSAGE_TYPE_SYSTEM = "system"
local MESSAGE_TYPE_SESSION_READY = "session_ready"
local MESSAGE_TYPE_SESSION_RECOVERED = "session_recovered"
local MESSAGE_TYPE_SESSION_CLOSED = "session_closed"

-- Simple Session Process - Handles basic message processing
local function run(args)
    -- Validate required args
    if not args or not args.user_id or not args.session_id then
        error("User ID and session ID are required")
    end

    -- Initialize actor state
    local initial_state = {
        session_id = args.session_id,
        user_id = args.user_id,
        parent_pid = args.parent_pid,
        conn_pid = args.conn_pid,
        start_token = args.start_token,
        start_context = args.start_context,
        create = args.create,
    }

    local loaded_state
    local err

    if initial_state.create then
        -- Create new session
        if not initial_state.start_token then
            error("Start token is required for new session")
        end

        state, err = loader.create_session(initial_state)
    else
        -- Load existing session
        state, err = loader.load_session(initial_state)
        -- todo load msg
    end

    if err then
        error(err)
    end

    print("Session initialized:", state.session_id)

    -- Notify parent about session status
    if state.parent_pid then
        if state.create then
            -- New session
            process.send(state.parent_pid, UPDATE_TOPIC, {
                type = MESSAGE_TYPE_SESSION_READY,
                session_id = state.session_id,
                agent = state.meta.agent,
                model = state.meta.model,
                kind = state.meta.kind
            })
        else
            -- Recovered session
            process.send(state.parent_pid, UPDATE_TOPIC, {
                type = MESSAGE_TYPE_SESSION_RECOVERED,
                session_id = state.session_id,
                agent = state.meta.agent,
                model = state.meta.model,
                kind = state.meta.kind,
                last_message_id = state.last_message_id
            })
        end
    end



    -- Define message handlers
    local handlers = {
        -- Handle cancellation
        __on_cancel = function(state)
            print("Session cancelled:", state.session_id)
            state.is_active = false
            return actor.exit({ status = "shutdown" })
        end,

        -- Handle user messages
        [SESSION_MESSAGE_TOPIC] = function(state, payload)
            print("Session message received:", state.session_id, json.encode(payload))

            -- Add message to history
            table.insert(state.messages, payload)

            -- Simple echo response for testing
            if payload.conn_pid then
                -- Notify that we're starting to process
                process.send(payload.conn_pid, UPDATE_TOPIC, {
                    type = MESSAGE_TYPE_START,
                    session_id = state.session_id,
                    model = state.meta.model,
                    provider = state.meta.provider
                })

                -- Simulate thinking (very simplified)
                process.send(payload.conn_pid, UPDATE_TOPIC, {
                    type = MESSAGE_TYPE_THINKING,
                    session_id = state.session_id,
                    content = "Processing your message..."
                })

                -- Add a slight delay to simulate processing
                time.sleep("500ms")

                -- Send content in chunks to simulate streaming
                local response = "This is a simple echo response from the session process.\n\n"

                -- If the user's message contains content, echo it back
                if payload.data then
                    response = response ..
                        "You said: " ..
                        (type(payload.data) == "table" and json.encode(payload.data) or tostring(payload.data)) .. "\n\n"
                end

                -- Simulate streaming by sending chunks
                local chunks = {
                    response:sub(1, 20),
                    response:sub(21, 50),
                    response:sub(51)
                }

                for _, chunk in ipairs(chunks) do
                    process.send(payload.conn_pid, UPDATE_TOPIC, {
                        type = MESSAGE_TYPE_CONTENT,
                        session_id = state.session_id,
                        content = chunk
                    })
                    time.sleep("100ms")
                end

                -- Finish the response
                process.send(payload.conn_pid, UPDATE_TOPIC, {
                    type = MESSAGE_TYPE_DONE,
                    session_id = state.session_id,
                    model = state.meta.model,
                    provider = state.meta.provider,
                    tokens = {
                        prompt = 10,
                        completion = 30,
                        total = 40
                    }
                })
            end

            return state
        end,

        -- Handle commands
        [SESSION_COMMAND_TOPIC] = function(state, payload)
            print("Session command received:", state.session_id, json.encode(payload))

            -- Command could be: stop, model, system, etc.
            local command = payload.command
            local params = payload.params or {}

            if command == "stop" then
                -- Just acknowledge the stop command
                process.send(payload.conn_pid, UPDATE_TOPIC, {
                    type = MESSAGE_TYPE_SYSTEM,
                    session_id = state.session_id,
                    message = "Generation stopped"
                })
            elseif command == "model" then
                -- handle model change
                if payload.data and payload.data.name then
                    state.meta.model = payload.data.name
                    process.send(payload.conn_pid, UPDATE_TOPIC, {
                        type = MESSAGE_TYPE_SYSTEM,
                        session_id = state.session_id,
                        message = "Model changed to " .. payload.data.name
                    })
                end
            end

            return state
        end
    }

    -- Create and run the actor
    return actor.new(initial_state, handlers).run()
end

return { run = run }

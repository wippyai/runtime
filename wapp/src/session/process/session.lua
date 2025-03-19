local time = require("time")
local json = require("json")
local actor = require("actor")

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
local MESSAGE_TYPE_SESSION_CLOSED = "session_closed"

-- Simple Session Process - Handles basic message processing
local function run(args)
    -- Validate required args
    if not args or not args.session_id or not args.user_id then
        return { error = "Missing required arguments" }
    end

    -- Initialize actor state
    local initial_state = {
        session_id = args.session_id,
        user_id = args.user_id,
        parent_pid = args.parent_pid,
        context_id = args.primary_context_id,
        start_time = time.now(),
        messages = {},
        is_active = true,
        meta = {
            model = "claude-3-7-sonnet-20250219",
            provider = "anthropic"
        }
    }

    -- Define message handlers
    local handlers = {
        -- Initialize the actor
        __init = function(state)
            print("Session started:", state.session_id)

            -- Notify parent that we're ready
            process.send(state.parent_pid, UPDATE_TOPIC, {
                type = MESSAGE_TYPE_SESSION_READY,
                session_id = state.session_id
            })

            -- todo load session or create it, notify client about last message id if any
            -- todo: notify about status also

            return state
        end,

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
                if payload.content then
                    response = response .. "You said: " .. payload.content .. "\n\n"
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
            end

            return state
        end
    }

    -- Create and run the actor
    return actor.new(initial_state, handlers).run()
end

return { run = run }

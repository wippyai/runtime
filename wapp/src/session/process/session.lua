local time = require("time")
local json = require("json")
local actor = require("actor")

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
        conn_pid = args.conn_pid,
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
            if state.parent_pid then
                process.send(state.parent_pid, "update", {
                    type = "session_ready",
                    session_id = state.session_id
                })
            end

            return state
        end,

        -- Handle cancellation
        __on_cancel = function(state)
            print("Session cancelled:", state.session_id)
            state.is_active = false

            -- Notify clients of termination
            if state.conn_pid then
                process.send(state.conn_pid, "update", {
                    type = "session_closed",
                    session_id = state.session_id,
                    reason = "cancelled"
                })
            end

            return actor.exit({ status = "shutdown" })
        end,

        -- Handle user messages
        ["session.message"] = function(state, payload)
            print("Session message received:", state.session_id)

            -- Add message to history
            table.insert(state.messages, payload)

            -- Simple echo response for testing
            if state.conn_pid then
                -- Notify that we're starting to process
                process.send(state.conn_pid, "update", {
                    type = "start",
                    session_id = state.session_id,
                    model = state.meta.model,
                    provider = state.meta.provider
                })

                -- Simulate thinking (very simplified)
                process.send(state.conn_pid, "update", {
                    type = "thinking",
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
                    process.send(state.conn_pid, "update", {
                        type = "content",
                        session_id = state.session_id,
                        content = chunk
                    })
                    time.sleep("100ms")
                end

                -- Finish the response
                process.send(state.conn_pid, "update", {
                    type = "done",
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
        ["session.command"] = function(state, payload)
            print("Session command received:", state.session_id, json.encode(payload))

            -- Command could be: stop, model, system, etc.
            local command = payload.command
            local params = payload.params or {}

            if command == "stop" then
                -- Just acknowledge the stop command
                process.send(state.conn_pid, "update", {
                    type = "system",
                    session_id = state.session_id,
                    message = "Generation stopped"
                })
            elseif command == "model" then
                -- Update the model if provided
                if params.name then
                    state.meta.model = params.name

                    -- Update provider based on model name pattern
                    if string.find(params.name, "claude") then
                        state.meta.provider = "anthropic"
                    elseif string.find(params.name, "gpt") then
                        state.meta.provider = "openai"
                    end

                    -- Acknowledge the model change
                    process.send(state.conn_pid, "update", {
                        type = "system",
                        session_id = state.session_id,
                        message = "Model changed to " .. state.meta.model
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

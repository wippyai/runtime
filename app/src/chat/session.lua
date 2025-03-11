local actor = require("actor")
local time = require("time")
local json = require("json")
local funcs = require("funcs")

local function run(args)
    local state = {
        pid = process.pid(),
        created_at = time.now(),
        messages = {},
        current_response = "",
        is_responding = false
    }


    local session = actor.new(state, {
        __init = function(state)
            print("Chat session initiated:", state.pid, tostring(time.now()))
        end,

        message = function(state, msg)
            print("Chat message from", msg.from, ":", msg.text)

            -- Add user message to history
            table.insert(state.messages, {
                role = "user",
                content = msg.text,
                timestamp = time.now()
            })

            -- Use reply_to if available; otherwise fall back to msg.from.
            local target = msg.reply_to or msg.from

            local response, err = funcs.new():call("app.funcs.openai:llm_query", {
                message = msg.text,
                history = state.messages,
                stream = (target ~= nil),
                reply_to = target,
                endpoint = "https://api.openai.com/v1/chat/completions",
                model = "gpt-4o-mini"
            })

            if err then
                if target then
                    process.send(target, "response", {
                        error = err,
                        done = true
                    })
                end
                return
            end

            -- Add AI response to history
            table.insert(state.messages, {
                role = "assistant",
                content = response,
                timestamp = time.now()
            })

            -- For non-streaming mode (when no reply target is provided), we return the response.
            if not target then
                return response
            end
            -- In streaming mode, llm_query.lua has already sent the response chunks.
        end,

        get_history = function(state, msg)
            if msg.reply_to then
                process.send(msg.reply_to, "response", { history = state.messages })
            end
            return state.messages
        end,

        clear_history = function(state)
            state.messages = {}
        end,

        __on_cancel = function(state)
            print("Session received cancel signal")
            if args and args.manager_pid then
                process.send(args.manager_pid, "session_closed", {
                    pid = state.pid,
                    reason = "cancelled"
                })
            end
            return actor.exit({ status = "shutdown" })
        end
    })

    return session.run()
end

return { run = run }

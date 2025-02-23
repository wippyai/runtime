local actor = require("actor")
local time = require("time")
local json = require("json")

local function run()
    local state = {
        pid = process.pid(),
        sessions = {} -- Track active sessions
    }

    print("Chat session manager started with PID:", state.pid)

    local manager = actor.new(state, {
        -- Handle session creation requests
        create_session = function(state, msg)
            print("Creating new session, request from:", msg.from)

            -- Spawn new monitored session
            local session_pid = process.spawn_monitored(
                "chat:session.proc",
                "system:heap",
                {
                    manager_pid = state.pid
                }
            )

            if not session_pid then
                print("Failed to create session")
                if msg.reply_to then
                    process.send(msg.reply_to, "response", {
                        status = "error",
                        error = "Failed to create session"
                    })
                end
                return
            end

            -- Track session
            state.sessions[session_pid] = {
                created_at = time.now(),
                created_by = msg.from
            }

            print("Created new session:", session_pid)

            if msg.reply_to then
                process.send(msg.reply_to, "response", {
                    status = "ok",
                    session_pid = session_pid
                })
            end
        end,

        -- Handle session closure notifications
        session_closed = function(state, msg)
            print("Session closed:", msg.pid, "reason:", msg.reason)
            state.sessions[msg.pid] = nil
        end,

        -- Handle monitored process events
        __on_event = function(state, event)
            if event.event.kind == process.EVENT_RESULT then
                local pid = event.event.from
                if state.sessions[pid] then
                    print("Session terminated:", pid, "error:", event.event.error or "none")
                    state.sessions[pid] = nil
                end
            end
        end,

        -- Handle cancellation
        on_cancel = function(state)
            print("Session manager received cancel signal")
            return actor.exit({ status = "shutdown" })
        end,

        -- Default handler for unknown messages
        __default = function(state, msg, topic)
            print("Unknown message received:", topic)
            print("Payload:", json.encode(msg))
        end
    })

    return manager.run()
end

return { run = run }

local actor = require("actor")
local time = require("time")
local json = require("json")

local function run()
    local state = {
        pid = process.pid(),
        sessions = {} -- Track active sessions
    }

    process.registry.register("chat_session_manager")

    print("Chat session manager started with PID:", state.pid)

    local manager = actor.new(state, {
        create_session = function(state, msg)
            print("Creating new session, request from:", msg.from)

            -- Spawn new monitored session
            local session_pid = process.spawn_monitored(
                "app.service.chat:session",
                "system:processes",
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

        __on_event = function(state, event)
            if event.kind == process.event.EXIT then
                local pid = event.from
                if state.sessions[pid] then
                    print("Session terminated:", pid, "error:", event.error or "none")
                    state.sessions[pid] = nil
                end
            end
        end,

        __on_cancel = function(state)
            print("Session manager received cancel signal")
            return actor.exit({ status = "shutdown" })
        end,

        __default = function(state, msg, topic)
            print("Unknown message received:", topic)
            print("Payload:", json.encode(msg))
        end
    })

    return manager.run()
end

return { run = run }

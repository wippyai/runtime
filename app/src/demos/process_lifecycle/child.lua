local actor = require("actor")
local time = require("time")

local function run(args)
    local state = {
        pid = process.pid(),
        parent_pid = args.parent_pid,
        start_time = time.now(),
        status = "running",
        counter = 0
    }

    print("Child process started with PID:", state.pid)
    print("Parent PID:", state.parent_pid)

    -- Create work simulation timer
    local work_timer = time.interval("1s")

    local child_actor = actor.new(state, {
        -- Handle status request
        get_status = function(state, msg)
            if msg.reply_to then
                process.send(msg.reply_to, "status", {
                    pid = state.pid,
                    status = state.status,
                    uptime = time.now():sub(state.start_time),
                    parent_pid = state.parent_pid,
                    counter = state.counter
                })
            end
        end,

        -- Handle simulated work via timer
        __on_timer = function(state, timer)
            if timer == work_timer then
                state.counter = state.counter + 1
                print("Child", state.pid, "working... count:", state.counter)

                -- Report progress to parent
                if state.parent_pid then
                    process.send(state.parent_pid, "child_progress", {
                        pid = state.pid,
                        counter = state.counter
                    })
                end
            end
        end,

        -- Handle cancellation
        on_cancel = function(state)
            print("Child received cancel event")
            state.status = "cancelling"

            -- Simulate cleanup work
            print("Child doing cleanup...")
            time.sleep("500ms")
            print("Child cleanup completed")

            -- Clean shutdown
            return actor.exit({ status = "cancelled", counter = state.counter })
        end
    })

    -- Run the actor loop
    local result = child_actor.run()
    print("Child process exiting:", state.pid)
    return result
end

return { run = run }
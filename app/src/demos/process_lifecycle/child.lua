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

    --print("Child process finished with PID:", state.pid)
    return "OK"

    --print("Child process started with PID:", state.pid)
    --print("Parent PID:", state.parent_pid)
    --
    ---- Create work simulation ticker (correct API)
    --local work_ticker = time.ticker("100ms")
    --
    ---- Create a deadline timer that will shut down the process after 30s
    --local deadline_timer = time.timer("10s")
    --
    --local child_actor = actor.new(state, {
    --    -- Initialize function (register channels)
    --    __init = function(state)
    --        -- Register work ticker channel
    --        state.register_channel(work_ticker:channel(), function(state, value, ok)
    --            if ok then
    --                state.counter = state.counter + 1
    --                --print("Child", state.pid, "working... count:", state.counter)
    --
    --                -- Report progress to parent
    --                if state.parent_pid then
    --                    process.send(state.parent_pid, "child_progress", {
    --                        pid = state.pid,
    --                        counter = state.counter
    --                    })
    --                end
    --            else
    --                print("Work ticker channel closed")
    --            end
    --        end)
    --
    --        -- Register deadline timer channel
    --        state.register_channel(deadline_timer:channel(), function(state, value, ok)
    --            if ok then
    --                print("Child deadline reached after 30s, shutting down...")
    --                state.status = "deadline_reached"
    --                return actor.exit({ status = "deadline_reached", counter = state.counter })
    --            end
    --        end)
    --    end,
    --
    --    -- Handle status request
    --    get_status = function(state, msg)
    --        if msg.reply_to then
    --            process.send(msg.reply_to, "status", {
    --                pid = state.pid,
    --                status = state.status,
    --                uptime = time.now():sub(state.start_time),
    --                parent_pid = state.parent_pid,
    --                counter = state.counter
    --            })
    --        end
    --    end,
    --
    --    -- Handle cancellation
    --    __on_cancel = function(state)
    --        print("Child received cancel event")
    --        state.status = "cancelling"
    --
    --        -- Stop the ticker and timer
    --        work_ticker:stop()
    --        deadline_timer:stop()
    --
    --        -- Simulate cleanup work
    --        print("Child doing cleanup...")
    --        time.sleep("500ms")
    --        print("Child cleanup completed")
    --
    --        -- Clean shutdown
    --        return actor.exit({ status = "cancelled", counter = state.counter })
    --    end
    --})
    --
    ---- Run the actor loop
    --local result = child_actor.run()
    --print("Child process exiting:", state.pid)
    --return result
end

return { run = run }

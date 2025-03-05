local actor = require("actor")
local time = require("time")

local function run(args)
    local state = {
        pid = process.pid(),
        start_time = time.now(),
        child_pid = nil,
        status = "running",
        counter = 0
    }

    process.registry.register("parent_" .. state.pid)
    print("Parent process started with PID:", state.pid)

    -- Set up trap_links to receive LINK_DOWN events instead of dying
    process.set_options({ trap_links = true })

    -- Spawn a linked child process - if child crashes, parent will be notified
    state.child_pid = process.spawn_linked_monitored(
        "app.demos.process_lifecycle:child",
        "system:processes",
        { parent_pid = state.pid }
    )

    if not state.child_pid then
        print("Failed to spawn child process")
        return { status = "error", error = "Failed to spawn child" }
    end

    print("Spawned linked child:", state.child_pid)

    local parent_actor = actor.new(state, {
        -- Handle status request
        get_status = function(state, msg)
            if msg.reply_to then
                process.send(msg.reply_to, "status", {
                    pid = state.pid,
                    status = state.status,
                    uptime = time.now():sub(state.start_time),
                    child_pid = state.child_pid,
                    counter = state.counter
                })
            end
        end,

        -- Handle child progress updates
        child_progress = function(state, msg)
            --print("Child progress:", msg.counter)
            state.counter = msg.counter
        end,

        -- Handle system events
        __on_event = function(state, event)
            if event.kind == process.event.LINK_DOWN then
                -- This happens when a linked process dies, and we have trap_links=true
                print("Child process down:", event.from)
                state.status = "child_down"
                error("children down, we can not continue")
            elseif event.kind == process.event.EXIT then
                print("Child process compete:", event.result.result)
                state.status = "child_complete"
                return actor.exit({ status = "complete", counter = state.counter })
            elseif event.kind == process.event.CANCEL then
                print("Cancel requested with deadline:", event.deadline)
                state.status = "cancelling"

                -- Forward cancellation to child process
                if state.child_pid then
                    print("Forwarding cancel to child:", state.child_pid)
                    process.cancel(state.child_pid, event.deadline)
                end

                -- Clean shutdown - we can do cleanup work here
                return actor.exit({ status = "cancelled", counter = state.counter })
            end
        end,

        -- Handle cancellation via direct call
        __on_cancel = function(state)
            print("Parent process received direct cancel")
            state.status = "cancelling"

            -- Forward cancellation to child process
            if state.child_pid then
                process.cancel(state.child_pid, "2s")
            end

            -- Give child time to clean up
            time.sleep("1s")

            return actor.exit({ status = "cancelled" })
        end
    })

    -- Run the actor loop
    local result = parent_actor.run()
    print("Parent process exiting:", state.pid)
    return result
end

return { run = run }

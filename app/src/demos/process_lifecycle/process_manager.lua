local actor = require("actor")
local time = require("time")
local json = require("json")

local function run()
    local state = {
        pid = process.pid(),
        processes = {},          -- Process tracking
        next_id = 1,             -- For generating unique IDs
        terminated_count = 0,    -- Count of terminated processes
        completed_processes = {} -- Archive of completed processes
    }

    print("Process lifecycle manager started with PID:", state.pid)
    process.registry.register("process_lifecycle_manager")

    local manager = actor.new(state, {
        -- Create a new parent process
        create_process = function(state, msg)
            local id = state.next_id
            state.next_id = state.next_id + 1

            --print("Creating new parent process, request from:", msg.from, state.next_id)

            ---- Spawn new parent process (monitored by us)
            local parent_pid, err = process.spawn_monitored(
                "app.demos.process_lifecycle:parent",
                "system:processes"
            )

            if not parent_pid then
                print("Failed to create parent process", err)
                if msg.reply_to then
                    process.send(msg.reply_to, "response", {
                        status = "error",
                        error = "Failed to create parent process"
                    })
                end
                return
            end

            -- Track process
            state.processes[id] = {
                id = id,
                parent_pid = parent_pid,
                created_at = time.now(),
                created_by = msg.from,
                status = "running"
            }

            --print("Created new parent process:", parent_pid, "ID:", id)

            if msg.reply_to then
                process.send(msg.reply_to, "response", {
                    status = "ok",
                    id = id,
                    parent_pid = parent_pid
                })
            end
        end,

        -- Cancel a process
        cancel_process = function(state, msg)
            local id = msg.id
            if not id or not state.processes[id] then
                if msg.reply_to then
                    process.send(msg.reply_to, "response", {
                        status = "error",
                        error = "Process not found"
                    })
                end
                return
            end

            local proc = state.processes[id]
            print("Cancelling process:", proc.parent_pid)

            local deadline = msg.deadline or "5s"
            local ok = process.cancel(proc.parent_pid, deadline)

            proc.status = "cancelling"

            if msg.reply_to then
                process.send(msg.reply_to, "response", {
                    status = ok and "ok" or "error",
                    id = id,
                    parent_pid = proc.parent_pid,
                    deadline = deadline
                })
            end
        end,

        -- Terminate a process (immediate kill)
        terminate_process = function(state, msg)
            local id = msg.id
            if not id or not state.processes[id] then
                if msg.reply_to then
                    process.send(msg.reply_to, "response", {
                        status = "error",
                        error = "Process not found"
                    })
                end
                return
            end

            local proc = state.processes[id]
            print("Terminating process:", proc.parent_pid)

            local ok = process.terminate(proc.parent_pid)

            proc.status = "terminating"
            state.terminated_count = state.terminated_count + 1

            print("Terminated count:", state.terminated_count)

            -- Crash after terminating 2 parents to test auto recovery
            if state.terminated_count >= 2 then
                print("Critical error: Too many terminated processes!")
                error("Process manager failure: Terminated over 2 parent processes")
            end

            if msg.reply_to then
                process.send(msg.reply_to, "response", {
                    status = ok and "ok" or "error",
                    id = id,
                    parent_pid = proc.parent_pid
                })
            end
        end,

        -- Get status of all processes
        get_processes = function(state, msg)
            local result = {}
            for id, proc in pairs(state.processes) do
                table.insert(result, {
                    id = id,
                    parent_pid = proc.parent_pid,
                    created_at = proc.created_at,
                    status = proc.status,
                    uptime = time.now():sub(proc.created_at),
                    result = proc.result
                })
            end

            if msg.reply_to then
                process.send(msg.reply_to, "response", {
                    status = "ok",
                    processes = result,
                    terminated_count = state.terminated_count,
                    completed_count = #state.completed_processes
                })
            end
        end,

        -- Get completed processes history
        get_completed_processes = function(state, msg)
            if msg.reply_to then
                process.send(msg.reply_to, "response", {
                    status = "ok",
                    completed_processes = state.completed_processes
                })
            end
        end,

        -- Handle monitored process events
        __on_event = function(state, event)
            if event.kind == process.event.EXIT then
                local pid = event.from

                -- Find which process completed
                for id, proc in pairs(state.processes) do
                    if proc.parent_pid == pid then
                        local status = "completed"

                        if event.result and event.result.error then
                            status = "failed"
                            --print("Process", pid, "failed:", event.result.error)
                        else
                            --print("Process", pid, "completed with result:", json.encode(event.result))
                        end

                        -- Update process status
                        proc.status = status
                        proc.result = event.result
                        proc.ended_at = time.now()

                        -- Archive the completed process
                        --table.insert(state.completed_processes, {
                        --    id = id,
                        --    parent_pid = proc.parent_pid,
                        --    created_at = proc.created_at,
                        --    ended_at = proc.ended_at,
                        --    status = status,
                        --    result = proc.result
                        --})

                        -- Remove process from active processes
                        state.processes[id] = nil
                        --print("Process", pid, "removed from active processes")

                        break
                    end
                end
            end
        end,

        -- Handle cancellation
        __on_cancel = function(state)
            print("Process manager received cancel signal")
            return actor.exit({ status = "shutdown" })
        end
    })

    return manager.run()
end

return { run = run }

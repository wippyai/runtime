local time = require("time")
local json = require("json")

local function run()
    local parent_pid = process.pid()
    print("Starting parent process", parent_pid)

    -- Configuration
    local MAX_CONCURRENT_CHILDREN = 500
    local TOTAL_CHILDREN = 100000
    local BATCH_REPORT_SIZE = 1000

    -- Process tracking
    local active_pids = {}
    local completed_count = 0
    local failed_count = 0
    local start_time = time.now()

    -- Channel setup
    local events_ch = process.events()
    local child_msgs = process.listen("child_msgs")
    local done = channel.new()
    local is_running = true
    local child_count = 0
    local active_children = 0

    local function log_progress()
        local elapsed = time.now():sub(start_time)
        local elapsed_seconds = elapsed:seconds()
        if elapsed_seconds > 0 then
            local completion_rate = (completed_count / elapsed_seconds)
            print(string.format(
                "Progress: %d/%d completed, %d failed (%.1f%%) - %.1f procs/sec, %d active",
                completed_count,
                TOTAL_CHILDREN,
                failed_count,
                (completed_count / TOTAL_CHILDREN) * 100,
                completion_rate,
                active_children
            ))
        end
    end

    local function spawn_child()
        if child_count >= TOTAL_CHILDREN then
            return false
        end

        child_count = child_count + 1
        active_children = active_children + 1

        local child_pid = process.spawn_monitored(
            "supervisor:child",
            "system:heap",
            {
                name = string.format("child_%d", child_count),
                start_time = time.now():format("15:04:05"),
                child_number = child_count,
                parent_pid = parent_pid,
                batch = math.floor(child_count / BATCH_REPORT_SIZE) + 1
            }
        )

        if not child_pid then
            print("Failed to spawn child process", child_count)
            active_children = active_children - 1
            failed_count = failed_count + 1
            return false
        end

        active_pids[child_pid] = {
            start_time = time.now(),
            child_number = child_count
        }
        return true
    end

    -- Initial batch spawn
    local spawned = 0
    while spawned < MAX_CONCURRENT_CHILDREN and child_count < TOTAL_CHILDREN do
        if spawn_child() then
            spawned = spawned + 1
        end
        -- Small delay between spawns to prevent overwhelming the system
        time.sleep("1ms")
    end

    -- Main event loop
    while is_running do
        local result = channel.select({
            events_ch:case_receive(),
            child_msgs:case_receive(),
            done:case_receive()
        })

        if not result.ok then
            break
        end

        if result.channel == events_ch and result.value then
            local event = result.value
            if event.event.kind == process.EVENT_CANCEL then
                print("Parent process received cancel signal")
                is_running = false
            elseif event.event.kind == process.EVENT_RESULT then
                local pid = event.event.from
                local proc_info = active_pids[pid]
                active_children = active_children - 1
                active_pids[pid] = nil

                if event.event.error then
                    failed_count = failed_count + 1
                    print(string.format("Child %d failed: %s",
                        proc_info and proc_info.child_number or "unknown",
                        event.event.error
                    ))
                else
                    completed_count = completed_count + 1
                end

                -- Spawn next child if needed
                if child_count < TOTAL_CHILDREN then
                    spawn_child()
                end

                -- Log progress on batch completion
                if (completed_count + failed_count) % BATCH_REPORT_SIZE == 0 then
                    log_progress()
                end
            end
        elseif result.channel == child_msgs and result.value then
            local msg = result.value
            -- Handle completion messages
            if msg.status == "completed" then
                -- Optional: Add any specific handling for completion messages
            end
            print(string.format("Message from child %d: %s", msg.child_number, json.encode(msg)))
        end

        -- Check if we're done
        if completed_count + failed_count >= TOTAL_CHILDREN then
            log_progress()
            local total_runtime = time.now():sub(start_time)
            print(string.format("\nBatch processing complete!\nTotal runtime: %s\nSuccessful: %d\nFailed: %d",
                tostring(total_runtime),
                completed_count,
                failed_count
            ))
            is_running = false
        end
    end

    -- Cleanup
    done:close()

    return {
        total_processed = completed_count + failed_count,
        successful = completed_count,
        failed = failed_count,
        runtime = tostring(time.now():sub(start_time))
    }
end

return {
    run = run
}
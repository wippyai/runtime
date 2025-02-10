local wf = require "temporal_workflow"

local activities = wf.init_activities({
    hello_world = {
        name = "hello_world.activity",
        config = {
            task_queue = "wippy_demos",
            schedule_to_close_timeout = "10s"
        }
    },
})

-- Helper function to process tasks from work channel
local function worker(work_channel, results_channel)
    while true do
        local task_index, ok = work_channel:receive()
        if not ok then
            break
        end
        local result = activities.hello_world("Hello", "World " .. task_index):await()
        results_channel:send({index = task_index, result = result.message})
    end
end

-- Helper function to calculate number of workers based on sine wave
local function calculate_workers(step, min_workers, max_workers, total_steps)
    local position = (step * 2 * math.pi) / total_steps
    local sine = math.sin(position)
    local worker_range = max_workers - min_workers
    local workers = math.floor(min_workers + (worker_range / 2) * (sine + 1))
    return math.max(1, workers)
end

-- Helper function to create visual scale with processed count
local function create_scale(current_workers, max_workers, width, processed_count, total_tasks)
    local fill_count = math.floor((current_workers * width) / max_workers)
    local empty_count = width - fill_count
    return string.format("[%s%s] %d/%d workers | Processed: %d/%d",
        string.rep("=", fill_count),
        string.rep(" ", empty_count),
        current_workers,
        max_workers,
        processed_count,
        total_tasks)
end

function execute_workflow()
    local NUM_TASKS = 1000
    local MIN_WORKERS = 1
    local MAX_WORKERS = 50
    local TOTAL_STEPS = 200  -- 20 seconds with 100ms intervals = 200 steps
    local SCALING_INTERVAL = "100ms"
    local SCALE_WIDTH = 30   -- Width of the visual scale

    -- Create buffered channels
    local work_channel = channel.new(NUM_TASKS)
    local results_channel = channel.new(NUM_TASKS)

    -- Prefill work channel
    for i = 1, NUM_TASKS do
        work_channel:send(i)
    end
    work_channel:close()

    -- Track active workers and results
    local active_workers = {}
    local results = {}
    local received_count = 0

    -- Start worker manager coroutine
    coroutine.spawn(function()
        local step = 0
        local scale_timer = wf.sleep(SCALING_INTERVAL)

        while step < TOTAL_STEPS and received_count < NUM_TASKS do
            scale_timer:await()
            step = step + 1

            local target_workers = calculate_workers(
                step,
                MIN_WORKERS,
                MAX_WORKERS,
                TOTAL_STEPS
            )

            -- Adjust worker count (remove excess workers first)
            while #active_workers > target_workers do
                table.remove(active_workers)
            end

            -- Add new workers if needed
            while #active_workers < target_workers do
                local worker_id = #active_workers + 1
                table.insert(active_workers, worker_id)

                coroutine.spawn(function()
                    worker(work_channel, results_channel)
                end)
            end

            -- Print visual scale with processed count
            print(create_scale(#active_workers, MAX_WORKERS, SCALE_WIDTH, received_count, NUM_TASKS))

            -- Check for new results
            local result = channel.select{
                results_channel:case_receive(),
                default = true
            }

            if result.value then
                results[result.value.index] = result.value.result
                received_count = received_count + 1
            end

            -- Set up next scale timer
            scale_timer = wf.sleep(SCALING_INTERVAL)
        end
    end)

    -- Collect remaining results
    while received_count < NUM_TASKS do
        local result = results_channel:receive()
        results[result.index] = result.result
        received_count = received_count + 1
    end

    -- Combine results
    local combined_parts = {}
    for i = 1, NUM_TASKS do
        table.insert(combined_parts, results[i] or "missing")
    end

    -- print("Results: " .. table.concat(combined_parts, ", "))
    return table.concat(combined_parts, ", ")
end
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
local function worker(work_channel, results_channel, worker_id)
    while true do
        -- Get next task index or exit if channel closed
        local task_index, ok = work_channel:receive()
        if not ok then
            break
        end

        -- Execute activity and send result to results channel
        local result = activities.hello_world("Hello", "World " .. task_index):await()
        -- Extract message from result table
        results_channel:send({index = task_index, result = result.message})
    end
end

function execute_workflow()
    local NUM_WORKERS = 200
    local NUM_TASKS = 5000

    -- Create channels for work distribution and result collection
    local work_channel = channel.new(NUM_TASKS)  -- Buffered channel for work items
    local results_channel = channel.new(NUM_TASKS)  -- Buffered channel for results

    -- Spawn worker coroutines
    for i = 1, NUM_WORKERS do
        coroutine.spawn(function()
            worker(work_channel, results_channel, i)
        end)
    end

    -- Distribute work
    for i = 1, NUM_TASKS do
        work_channel:send(i)
    end
    work_channel:close()

    -- Collect all results
    local results = {}
    for i = 1, NUM_TASKS do
        local result = results_channel:receive()
        results[result.index] = result.result  -- Already a string from .message
    end

    -- Combine results into single string using table.concat
    local combined_parts = {}
    for i = 1, NUM_TASKS do
        table.insert(combined_parts, results[i])
    end

    return table.concat(combined_parts, ", ")
end
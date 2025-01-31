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

function execute_workflow()
    -- Create done channel and main timer
    local done_channel = channel.new()
    local main_timer = wf.sleep("5s")

    -- Start activity coroutine
    coroutine.spawn(function()
        while true do
            -- Execute activity first
            activities.hello_world("Hello", "World"):await()

            -- Check if we should continue after a small sleep
            local result = channel.select {
                done_channel:case_receive(),
                main_timer:response():case_receive(),
                wf.sleep("100ms"):response():case_receive(),
            }

            -- Exit if either done signal or main timer completed
            if result.channel == done_channel or result.channel == main_timer:response() then
                break
            end
        end
    end)

    -- Wait for main timer to complete
    main_timer:await()
    done_channel:close()

    return "Workflow completed"
end

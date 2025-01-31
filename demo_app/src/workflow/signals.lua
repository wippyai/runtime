local wf = require "temporal_workflow"

-- Initialize activities (we'll use this to echo received messages)
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
    -- Create subscriber for the "signal" topic
    local subscriber = pubsub.subscribe("signal")
    local messages_received = {}

    -- Process messages until timeout
    local workflow_ch = wf.sleep("10s"):response()

    pretty_print(workflow_ch)

    -- Main message processing loop
    while true do
        -- Wait for either a message or timeout
        local result = channel.select {
            subscriber:case_receive(),
            workflow_ch:case_receive()
        }

        -- Check if workflow timer completed
        if result.channel == workflow_ch then
            break
        end

        -- Process received message
        if result.value then
            table.insert(messages_received, result.value)

            print("Processed message: " .. result.value)
        end
    end

    -- Prepare final result
    local result = {
        total_messages = #messages_received,
        messages = messages_received
    }

    return result
end

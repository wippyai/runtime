local wf = require "temporal_workflow"

function execute_workflow()
    -- Create subscriber for the "signal" topic
    local subscriber = pubsub.subscribe("signal")
    local messages_received = {}

    -- Process messages until timeout
    local workflow_ch = wf.sleep("10s"):response()

    -- count of messages received
    local count = 0

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
            count = count + 1

            print("Processed message: " .. result.value)
        end
    end

    print("Received " .. count .. " messages")

    -- Prepare final result
    local result = {
        total_messages = #messages_received,
        messages = messages_received,
        count = count
    }

    return result
end

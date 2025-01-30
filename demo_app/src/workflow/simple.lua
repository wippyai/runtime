local wf = require "temporal_workflow"

function execute_workflow()
    -- Create an activity command with required parameters
    local activity_cmd = command.new(
        "activity",    -- command type
        "hello-world", -- activity name
        {              -- activity options
            taskQueue = "default_tq",
            startToCloseTimeout = "5s",
            scheduleToCloseTimeout = "10s"
        },
        "Hello", "World" -- activity arguments
    )

    -- Wait for activity response
    local response = activity_cmd:response()
    local result, ok = response:receive()

    if not ok then
        return { error = "Activity failed" }
    end

    return result
end

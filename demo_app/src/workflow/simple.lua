local wf = require "temporal_workflow"

function execute_workflow()
    -- Create an activity command with required parameters
    local activity_cmd = command.new(
        "activity",             -- command type
        "hello_world.activity", -- activity name
        {
            task_queue = "wippy_demos",
            start_to_close_timeout = "5s",
            schedule_to_close_timeout = "10s",
            retry_policy = {
                initial_interval = "1s",
                backoff_coefficient = 2.0,
                maximum_interval = "100s",
                maximum_attempts = 3
            }
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

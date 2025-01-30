function execute_workflow()
    -- Execute the hello world activity
    local activity_cmd = command.new(
        "activity",             -- command type
        "hello_world.activity", -- activity name
        {                       -- activity options
            taskQueue = "default_tq",
            startToCloseTimeout = "5s"
        },
        { message = "Hello from workflow!" } -- activity arguments
    )

    -- Wait for activity response
    local response = activity_cmd:response()
    local result, ok = response:receive()

    if not ok then
        return { error = "Activity failed" }
    end

    -- Create a timer command (wait for 2 seconds)
    local timer_cmd = command.new(
        "timer", -- command type
        {        -- timer options
            duration = "2s"
        }
    )

    -- Wait for timer
    local timer_response = timer_cmd:response()
    local _, timer_ok = timer_response:receive()

    if not timer_ok then
        return { error = "Timer failed" }
    end

    return result
end

local temporal = require("temporal")
local json = require("json")

-- Create a ProcessData activity with configuration
local process_data = temporal.activity("ProcessData", {
    task_queue = "simple-task-queue-2",
    start_to_close_timeout = "25s"
})

local function run(...)
    print("Starting workflow execution")

    -- Execute the activity with input data
    local handle = process_data({
        id = "123",
        name = "Test Data"
    })

    -- Wait for the activity to complete
    local result = handle:wait()

    if result then
        print("Activity succeeded with result:", json.encode(result))
    else
        local err = handle:error()
        if err then
            print("Activity failed:", tostring(err))
        else
            print("Activity completed with no result")
        end
    end

    -- Return whatever arguments we received
    return ...
end

return { run = run }

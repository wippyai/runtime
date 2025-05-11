local json = require("json")

local function run(...)
    print("we doing work")

    local cmd = workflow.command({
        type = "activity",
        params = {
            "ProcessData",
            {
                task_queue = "simple-task-queue-2",
                start_to_close_timeout = "25s",
            },
            {
                id = "123",
                name = "Test Data"
            }
        }
    })

    -- Wait for command response
    local result, err = cmd:response():receive()

    if err then
        print("Command failed: " .. tostring(err))
    else
        print("Command succeeded with result:", json.encode(result))
    end

    -- Example of using request with an external command:
    -- local externalCmd = some_function_that_creates_command()
    -- wf.request(externalCmd)

    -- Just return whatever arguments we received
    return ...
end

return { run = run }
